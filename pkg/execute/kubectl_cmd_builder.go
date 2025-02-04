package execute

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/google/uuid"
	"github.com/sirupsen/logrus"
	"github.com/slack-go/slack"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kubeshop/botkube/pkg/api"
	"github.com/kubeshop/botkube/pkg/bot/interactive"
	"github.com/kubeshop/botkube/pkg/config"
	"github.com/kubeshop/botkube/pkg/execute/kubectl"
)

const (
	verbsDropdownCommand             = "kc-cmd-builder --verbs"
	resourceTypesDropdownCommand     = "kc-cmd-builder --resource-type"
	resourceNamesDropdownCommand     = "kc-cmd-builder --resource-name"
	resourceNamespaceDropdownCommand = "kc-cmd-builder --namespace"
	filterPlaintextInputCommand      = "kc-cmd-builder --filter-query"
	kubectlCommandName               = "kubectl"
	dropdownItemsLimit               = 100
	noKubectlCommandsInChannel       = "No `kubectl` commands are enabled in this channel. To learn how to enable them, visit https://docs.botkube.io/configuration/executor."
	kubectlMissingCommandMsg         = "Please specify the kubectl command"
)

var knownCmdPrefix = map[string]struct{}{
	verbsDropdownCommand:             {},
	resourceTypesDropdownCommand:     {},
	resourceNamesDropdownCommand:     {},
	resourceNamespaceDropdownCommand: {},
	filterPlaintextInputCommand:      {},
}

var errRequiredVerbDropdown = errors.New("verbs dropdown select cannot be empty")

type (
	kcMerger interface {
		MergeAllEnabled(includeBindings []string) kubectl.EnabledKubectl
	}
	kcExecutor interface {
		Execute(bindings []string, command string, isAuthChannel bool, cmdCtx CommandContext) (string, error)
	}
	// NamespaceLister provides an option to list all namespaces in a given cluster.
	NamespaceLister interface {
		List(ctx context.Context, opts metav1.ListOptions) (*corev1.NamespaceList, error)
	}
)

// KubectlCmdBuilder provides functionality to handle interactive kubectl command selection.
type KubectlCmdBuilder struct {
	log             logrus.FieldLogger
	kcExecutor      kcExecutor
	merger          kcMerger
	namespaceLister NamespaceLister
	commandGuard    CommandGuard
}

// NewKubectlCmdBuilder returns a new KubectlCmdBuilder instance.
func NewKubectlCmdBuilder(log logrus.FieldLogger, merger kcMerger, executor kcExecutor, namespaceLister NamespaceLister, guard CommandGuard) *KubectlCmdBuilder {
	return &KubectlCmdBuilder{
		log:             log,
		kcExecutor:      executor,
		merger:          merger,
		namespaceLister: namespaceLister,
		commandGuard:    guard,
	}
}

// CanHandle returns true if it's allowed kubectl command that can be handled by this executor.
func (e *KubectlCmdBuilder) CanHandle(args []string) bool {
	// if we know the command prefix, we can handle that command :)
	return e.GetCommandPrefix(args) != ""
}

// GetCommandPrefix returns the command prefix only if it's known.
func (e *KubectlCmdBuilder) GetCommandPrefix(args []string) string {
	switch len(args) {
	case 0:
		return ""

	case 1:
		//  check if it's only a kubectl command without arguments
		if args[0] == kubectlCommandName {
			return args[0]
		}
		// it is a single arg which cannot start the kubectl command builder
		return ""

	default:
		// check if request comes from command builder message
		gotCmd := fmt.Sprintf("%s %s", args[0], args[1])
		if _, found := knownCmdPrefix[gotCmd]; found {
			return gotCmd
		}
		return ""
	}
}

// Do executes a given kc-cmd-builder command based on args.
//
// TODO: once we will have a real use-case, we should abstract the Slack state and introduce our own model.
func (e *KubectlCmdBuilder) Do(ctx context.Context, args []string, platform config.CommPlatformIntegration, bindings []string, state *slack.BlockActionStates, header string, cmdCtx CommandContext) (interactive.CoreMessage, error) {
	var empty interactive.CoreMessage

	if !platform.IsInteractive() {
		e.log.Debug("Interactive kubectl command builder is not supported on %s platform", platform)
		return e.message(header, kubectlMissingCommandMsg)
	}

	allVerbs, allTypes, defaultNs := e.getEnableKubectlDetails(bindings)
	if len(allVerbs) == 0 {
		return e.message(header, noKubectlCommandsInChannel)
	}

	allVerbs = e.commandGuard.FilterSupportedVerbs(allVerbs)

	// if only command name was specified, return initial command builder message
	if len(args) == 1 {
		return e.initialMessage(allVerbs)
	}

	stateDetails := e.extractStateDetails(state)
	if stateDetails.namespace == "" {
		stateDetails.namespace = defaultNs
	}

	var (
		cmdName = args[0]
		cmdVerb = args[1]
		cmd     = fmt.Sprintf("%s %s", cmdName, cmdVerb)
	)

	cmds := executorsRunner{
		verbsDropdownCommand: func() (interactive.CoreMessage, error) {
			return e.renderMessage(ctx, stateDetails, bindings, allVerbs, allTypes, cmdCtx)
		},
		resourceTypesDropdownCommand: func() (interactive.CoreMessage, error) {
			// the resource type was selected, so clear resource name from command preview.
			stateDetails.resourceName = ""
			return e.renderMessage(ctx, stateDetails, bindings, allVerbs, allTypes, cmdCtx)
		},
		resourceNamesDropdownCommand: func() (interactive.CoreMessage, error) {
			// this is called only when the resource name is directly selected from dropdown, so we need to include
			// it in command preview.
			return e.renderMessage(ctx, stateDetails, bindings, allVerbs, allTypes, cmdCtx)
		},
		resourceNamespaceDropdownCommand: func() (interactive.CoreMessage, error) {
			// when the namespace was changed, there is a small chance that resource name will be still matching,
			// we will need to do the external call to check that. For now, we clear resource name from command preview.
			stateDetails.resourceName = ""
			return e.renderMessage(ctx, stateDetails, bindings, allVerbs, allTypes, cmdCtx)
		},
		filterPlaintextInputCommand: func() (interactive.CoreMessage, error) {
			return e.renderMessage(ctx, stateDetails, bindings, allVerbs, allTypes, cmdCtx)
		},
	}

	msg, err := cmds.SelectAndRun(cmd)
	if err != nil {
		e.log.WithField("error", err.Error()).Error("Cannot render the kubectl command builder. Returning empty message.")
		return empty, err
	}
	return msg, nil
}

func (e *KubectlCmdBuilder) initialMessage(allVerbs []string) (interactive.CoreMessage, error) {
	var empty interactive.CoreMessage

	// We start a new interactive block, so we generate unique ID.
	// Later when we update this message with a new "body" e.g. update command preview
	// the block state remains the same as Slack always see it under the same id.
	// If we use different ID each time we update the message, Slack will clean up the state
	// meaning we will lose information about verb/resourceType/resourceName that were previously selected.
	id, err := uuid.NewRandom()
	if err != nil {
		return empty, err
	}
	allVerbsSelect := VerbSelect(allVerbs, "")
	if allVerbsSelect == nil {
		return empty, errRequiredVerbDropdown
	}

	msg := KubectlCmdBuilderMessage(id.String(), *allVerbsSelect)
	// we are the initial message, don't replace the original one as we need to send a brand-new message visible only to the user
	// otherwise we can replace a message that is publicly visible.
	msg.ReplaceOriginal = false

	return msg, nil
}

func (e *KubectlCmdBuilder) renderMessage(ctx context.Context, stateDetails stateDetails, bindings, allVerbs, allTypes []string, cmdCtx CommandContext) (interactive.CoreMessage, error) {
	var empty interactive.CoreMessage

	allVerbsSelect := VerbSelect(allVerbs, stateDetails.verb)
	if allVerbsSelect == nil {
		return empty, errRequiredVerbDropdown
	}

	// 1. Refresh resource type list
	matchingTypes, err := e.getAllowedResourcesSelectList(stateDetails.verb, allTypes, stateDetails.resourceType)
	if err != nil {
		return empty, err
	}

	// 2. If a given verb doesn't have assigned resource types,
	//    render:
	//      1. Dropdown with all verbs
	//      2. Filter input
	//      3. Command preview. For example:
	//           kubectl api-resources
	if matchingTypes == nil {
		// we must zero those fields as they are known only if we know the resource type and this verb doesn't have one :)
		stateDetails.resourceType = ""
		stateDetails.resourceName = ""
		stateDetails.namespace = ""
		preview := e.buildCommandPreview(stateDetails)

		return KubectlCmdBuilderMessage(
			stateDetails.dropdownsBlockID, *allVerbsSelect,
			WithAdditionalSections(preview...),
		), nil
	}

	// 3. If resource type is not on the listy anymore,
	//    render:
	//      1. Dropdown with all verbs
	//      2. Dropdown with all related resource types
	//    because we don't know the resource type we cannot render:
	//      1. Resource names - obvious :).
	//      2. Namespaces as we don't know if it's cluster or namespace scoped resource.
	if !e.contains(matchingTypes, stateDetails.resourceType) {
		return KubectlCmdBuilderMessage(
			stateDetails.dropdownsBlockID, *allVerbsSelect,
			WithAdditionalSelects(matchingTypes),
		), nil
	}

	// At this stage we know that:
	//   1. Verb requires resource types
	//   2. Selected resource type is still valid for the selected verb
	var (
		resNames = e.tryToGetResourceNamesSelect(bindings, stateDetails, cmdCtx)
		nsNames  = e.tryToGetNamespaceSelect(ctx, bindings, stateDetails)
	)

	// 4. If a given resource name is not on the list anymore, clear it.
	if !e.contains(resNames, stateDetails.resourceName) {
		stateDetails.resourceName = ""
	}

	// 5. If a given namespace is not on the list anymore, clear it.
	if !e.contains(nsNames, stateDetails.namespace) {
		stateDetails.namespace = ""
	}

	// 6. Render all dropdowns and full command preview.
	preview := e.buildCommandPreview(stateDetails)
	return KubectlCmdBuilderMessage(
		stateDetails.dropdownsBlockID, *allVerbsSelect,
		WithAdditionalSelects(matchingTypes, resNames, nsNames),
		WithAdditionalSections(preview...),
	), nil
}

func (e *KubectlCmdBuilder) tryToGetResourceNamesSelect(bindings []string, state stateDetails, cmdCtx CommandContext) *api.Select {
	if state.resourceType == "" {
		return EmptyResourceNameDropdown()
	}
	cmd := fmt.Sprintf(`%s get %s --ignore-not-found=true -o go-template='{{range .items}}{{.metadata.name}}{{"\n"}}{{end}}'`, kubectlCommandName, state.resourceType)
	if state.namespace != "" {
		cmd = fmt.Sprintf("%s -n %s", cmd, state.namespace)
	}

	out, err := e.kcExecutor.Execute(bindings, cmd, true, cmdCtx)
	if err != nil {
		e.log.WithField("error", err.Error()).Error("Cannot fetch resource names. Returning empty resource name dropdown.")
		return EmptyResourceNameDropdown()
	}

	lines := getNonEmptyLines(out)
	if len(lines) == 0 {
		return EmptyResourceNameDropdown()
	}

	return ResourceNamesSelect(overflowSentence(lines), state.resourceName)
}

func (e *KubectlCmdBuilder) tryToGetNamespaceSelect(ctx context.Context, bindings []string, details stateDetails) *api.Select {
	log := e.log.WithFields(logrus.Fields{
		"state":    details,
		"bindings": bindings,
	})

	resourceDetails, err := e.commandGuard.GetResourceDetails(details.verb, details.resourceType)
	if err != nil {
		log.WithField("error", err.Error()).Error("Cannot fetch resource details, ignoring namespace dropdown...")
		return nil
	}

	if !resourceDetails.Namespaced {
		log.Debug("Resource is not namespace-scoped, ignore namespace dropdown...")
		return nil
	}

	allClusterNamespaces, err := e.namespaceLister.List(ctx, metav1.ListOptions{
		Limit: dropdownItemsLimit,
	})
	if err != nil {
		log.WithField("error", err.Error()).Error("Cannot fetch all available Kubernetes namespaces, ignoring namespace dropdown...")
		return nil
	}

	var (
		kc        = e.merger.MergeAllEnabled(bindings)
		allowedNS = kc.AllowedNamespacesPerResource[details.resourceType]
		finalNS   []dropdownItem
	)

	initialNamespace := newDropdownItem(details.namespace, details.namespace)
	initialNamespace = e.appendNamespaceSuffixIfDefault(initialNamespace)

	for _, item := range allClusterNamespaces.Items {
		allowed, err := allowedNS.IsAllowed(item.Name)
		if err != nil {
			log.WithField("namespace", item.Name).
				WithField("error", err.Error()).
				Error("Cannot check if namespace is allowed, so skipping it.")
			continue
		}
		if !allowed {
			log.WithField("namespace", item.Name).Debug("Namespace is not allowed, so skipping it.")
			continue
		}

		kv := newDropdownItem(item.Name, item.Name)
		if initialNamespace.Value == kv.Value {
			kv = e.appendNamespaceSuffixIfDefault(kv)
		}

		finalNS = append(finalNS, kv)
	}

	return ResourceNamespaceSelect(finalNS, initialNamespace)
}

// UX requirement to append the (namespace) suffix if the namespace is called `default`.
func (e *KubectlCmdBuilder) appendNamespaceSuffixIfDefault(in dropdownItem) dropdownItem {
	if in.Name == "default" {
		in.Name += " (namespace)"
	}
	return in
}

func (e *KubectlCmdBuilder) getEnableKubectlDetails(bindings []string) (verbs []string, resources []string, namespace string) {
	enabledKubectls := e.merger.MergeAllEnabled(bindings)
	for key := range enabledKubectls.AllowedKubectlResource {
		resources = append(resources, key)
	}
	sort.Strings(resources)

	for key := range enabledKubectls.AllowedKubectlVerb {
		verbs = append(verbs, key)
	}
	sort.Strings(verbs)

	if enabledKubectls.DefaultNamespace == "" {
		enabledKubectls.DefaultNamespace = kubectlDefaultNamespace
	}

	return verbs, resources, enabledKubectls.DefaultNamespace
}

// getAllowedResourcesSelectList returns dropdown select with allowed resources for a given verb.
func (e *KubectlCmdBuilder) getAllowedResourcesSelectList(verb string, resources []string, resourceType string) (*api.Select, error) {
	allowedResources, err := e.commandGuard.GetAllowedResourcesForVerb(verb, resources)
	if err != nil {
		return nil, err
	}
	if len(allowedResources) == 0 {
		return nil, nil
	}

	allowedResourcesList := make([]string, 0, len(allowedResources))
	for _, item := range allowedResources {
		allowedResourcesList = append(allowedResourcesList, item.Name)
	}

	return ResourceTypeSelect(allowedResourcesList, resourceType), nil
}

type stateDetails struct {
	dropdownsBlockID string

	verb         string
	namespace    string
	resourceType string
	resourceName string
	filter       string
}

func (e *KubectlCmdBuilder) extractStateDetails(state *slack.BlockActionStates) stateDetails {
	if state == nil {
		return stateDetails{}
	}

	details := stateDetails{}
	for blockID, blocks := range state.Values {
		if !strings.Contains(blockID, filterPlaintextInputCommand) {
			details.dropdownsBlockID = blockID
		}
		for id, act := range blocks {
			id = strings.TrimSpace(id)

			switch id {
			case verbsDropdownCommand:
				details.verb = act.SelectedOption.Value
			case resourceTypesDropdownCommand:
				details.resourceType = act.SelectedOption.Value
			case resourceNamesDropdownCommand:
				details.resourceName = act.SelectedOption.Value
			case resourceNamespaceDropdownCommand:
				details.namespace = act.SelectedOption.Value
			case filterPlaintextInputCommand:
				details.filter = act.Value
			}
		}
	}
	return details
}

func (e *KubectlCmdBuilder) contains(matchingTypes *api.Select, resourceType string) bool {
	if matchingTypes == nil {
		return false
	}

	if matchingTypes.InitialOption != nil && matchingTypes.InitialOption.Value == resourceType {
		return true
	}

	return false
}

func (e *KubectlCmdBuilder) buildCommandPreview(state stateDetails) []api.Section {
	resourceDetails, err := e.commandGuard.GetResourceDetails(state.verb, state.resourceType)
	if err != nil {
		e.log.WithFields(logrus.Fields{
			"state": state,
			"error": err.Error(),
		}).Error("Cannot get resource details")
		return []api.Section{InternalErrorSection()}
	}

	if resourceDetails.SlashSeparatedInCommand && state.resourceName == "" {
		// we should not render the command as it will be invalid anyway without the resource name
		return nil
	}

	cmd := fmt.Sprintf("%s %s %s", kubectlCommandName, state.verb, state.resourceType)

	resourceNameSeparator := " "
	if resourceDetails.SlashSeparatedInCommand {
		// sometimes kubectl commands requires slash separator, without it, it will not work. For example:
		//   kubectl logs deploy/<deploy_name>
		resourceNameSeparator = "/"
	}

	if state.resourceName != "" {
		cmd = fmt.Sprintf("%s%s%s", cmd, resourceNameSeparator, state.resourceName)
	}

	if resourceDetails.Namespaced && state.namespace != "" {
		cmd = fmt.Sprintf("%s -n %s", cmd, state.namespace)
	}

	if state.filter != "" {
		cmd = fmt.Sprintf("%s --filter=%q", cmd, state.filter)
	}

	return PreviewSection(cmd, FilterSection())
}

func (e *KubectlCmdBuilder) message(header, msg string) (interactive.CoreMessage, error) {
	return interactive.CoreMessage{
		Description: header,
		Message: api.Message{
			BaseBody: api.Body{
				Plaintext: msg,
			},
		},
	}, nil
}

func splitByNewLines(c rune) bool {
	return c == '\n' || c == '\r'
}

func overflowSentence(in []string) []string {
	for idx := range in {
		if len(in[idx]) < 76 { // Maximum length for text field in dropdown is 75 characters. (https://api.slack.com/reference/block-kit/composition-objects#option)
			continue
		}

		in[idx] = in[idx][:72] + "..."
	}
	return in
}

func getNonEmptyLines(in string) []string {
	lines := strings.FieldsFunc(in, splitByNewLines)
	var out []string
	for _, x := range lines {
		if x == "" {
			continue
		}
		out = append(out, x)
	}
	return out
}
