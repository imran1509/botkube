package source

import (
	"context"
	"fmt"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"

	"github.com/kubeshop/botkube/pkg/api/source"
	"github.com/kubeshop/botkube/pkg/config"
)

type pluginDispatcher interface {
	Dispatch(ctx context.Context, pluginName string, pluginConfigs []*source.Config, sources []string) error
}

// Scheduler analyzes the provided configuration and based on that schedules plugin sources.
type Scheduler struct {
	log        logrus.FieldLogger
	cfg        *config.Config
	dispatcher pluginDispatcher

	// startProcesses holds information about started unique plugin processes
	// We start a new plugin process each time we see a new order of source bindings.
	// We do that because we pass the array of configs to each `Stream` method and
	// the merging strategy for configs can depend on the order.
	// As a result our key is e.g. ['source-name1;source-name2']
	startProcesses map[string]struct{}
}

// NewScheduler create a new Scheduler instance.
func NewScheduler(log logrus.FieldLogger, cfg *config.Config, dispatcher pluginDispatcher) *Scheduler {
	return &Scheduler{
		log:            log,
		cfg:            cfg,
		dispatcher:     dispatcher,
		startProcesses: map[string]struct{}{},
	}
}

// Start starts all sources and dispatch received events.
func (d *Scheduler) Start(ctx context.Context) error {
	for _, commGroupCfg := range d.cfg.Communications {
		if commGroupCfg.Slack.Enabled {
			for _, channel := range commGroupCfg.Slack.Channels {
				if err := d.schedule(ctx, channel.Bindings.Sources); err != nil {
					return err
				}
			}
		}

		if commGroupCfg.SocketSlack.Enabled {
			for _, channel := range commGroupCfg.SocketSlack.Channels {
				if err := d.schedule(ctx, channel.Bindings.Sources); err != nil {
					return err
				}
			}
		}

		if commGroupCfg.Mattermost.Enabled {
			for _, channel := range commGroupCfg.Mattermost.Channels {
				if err := d.schedule(ctx, channel.Bindings.Sources); err != nil {
					return err
				}
			}
		}

		if commGroupCfg.Teams.Enabled {
			if err := d.schedule(ctx, commGroupCfg.Teams.Bindings.Sources); err != nil {
				return err
			}
		}

		if commGroupCfg.Discord.Enabled {
			for _, channel := range commGroupCfg.Discord.Channels {
				if err := d.schedule(ctx, channel.Bindings.Sources); err != nil {
					return err
				}
			}
		}
	}

	return nil
}

func (d *Scheduler) schedule(ctx context.Context, bindSources []string) error {
	key := strings.Join(bindSources, ";")

	_, found := d.startProcesses[key]
	if found {
		return nil // such configuration was already started
	}
	d.startProcesses[key] = struct{}{}

	// Holds the array of configs for a given plugin.
	// For example, ['botkube/kubernetes@v1.0.0']->[]{"cfg1", "cfg2"}
	sourcePluginConfigs := map[string][]*source.Config{}
	for _, sourceCfgGroupName := range bindSources {
		plugins := d.cfg.Sources[sourceCfgGroupName].Plugins
		for pluginName, pluginCfg := range plugins {
			if !pluginCfg.Enabled {
				continue
			}

			// Unfortunately we need marshal it to get the raw data:
			// https://github.com/go-yaml/yaml/issues/13
			rawYAML, err := yaml.Marshal(pluginCfg.Config)
			if err != nil {
				return fmt.Errorf("while marshaling config for %s from source %s : %w", pluginName, sourceCfgGroupName, err)
			}
			sourcePluginConfigs[pluginName] = append(sourcePluginConfigs[pluginName], &source.Config{
				RawYAML: rawYAML,
			})
		}
	}

	for pluginName, configs := range sourcePluginConfigs {
		err := d.dispatcher.Dispatch(ctx, pluginName, configs, bindSources)
		if err != nil {
			return fmt.Errorf("while starting plugin source %s: %w", pluginName, err)
		}
	}
	return nil
}
