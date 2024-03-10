package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	coreconfig "github.com/DataDog/datadog-agent/comp/core/config"
	"github.com/DataDog/datadog-agent/comp/core/log"
	"github.com/DataDog/datadog-agent/comp/forwarder/defaultforwarder"
	"github.com/DataDog/datadog-agent/comp/forwarder/orchestrator/orchestratorinterface"
	"github.com/DataDog/datadog-agent/pkg/serializer"
	"go.opentelemetry.io/collector/component"
	"go.uber.org/fx"
	"go.uber.org/fx/fxevent"
	"go.uber.org/zap"
)

type API struct {
	Site string `mapstructure:"site"`
	Key  string `mapstructure:"key"`
}
type Config struct {
	API API `mapstructure:"api"`
}

func newLogComponent(set component.TelemetrySettings) (log.Component, error) {
	return &zaplogger{logger: set.Logger}, nil
}

func newSerializer(set component.TelemetrySettings, cfg *Config) (*serializer.Serializer, error) {
	var f defaultforwarder.Component
	var c coreconfig.Component
	var s *serializer.Serializer
	yamldata := fmt.Sprintf(`logs_enabled: true
log_level: %s
site: %s
api_key: %s
apm_config:
  enabled: true
  apm_non_local_traffic: true
forwarder_timeout: 10`, set.Logger.Level().String(), cfg.API.Site, cfg.API.Key)

	tempDir, err := os.MkdirTemp("", "conf")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(tempDir) // Clean up

	// Create a temporary file within tempDir
	tempFilePath := filepath.Join(tempDir, "datadog.yaml")
	tempFile, err := os.Create(tempFilePath)
	if err != nil {
		return nil, err
	}
	defer tempFile.Close()
	// Write data to the temp file
	if _, err := io.WriteString(tempFile, yamldata); err != nil {
		return nil, err
	}
	app := fx.New(
		fx.WithLogger(func(log *zap.Logger) fxevent.Logger {
			return &fxevent.ZapLogger{Logger: log}
		}),
		fx.Supply(set.Logger),
		coreconfig.Module(),
		fx.Provide(func() coreconfig.Params {
			return coreconfig.NewAgentParams(tempFilePath)
		}),

		fx.Supply(cfg),
		fx.Supply(set),
		fx.Provide(newLogComponent),

		fx.Provide(func(c coreconfig.Component, l log.Component) (defaultforwarder.Params, error) {
			return defaultforwarder.NewParams(c, l), nil
		}),
		fx.Provide(func(c defaultforwarder.Component) (defaultforwarder.Forwarder, error) {
			return defaultforwarder.Forwarder(c), nil
		}),
		fx.Provide(func() string {
			return ""
		}),
		fx.Provide(NewOrchestratorinterfaceimpl),
		fx.Provide(serializer.NewSerializer),
		defaultforwarder.Module(),
		fx.Populate(&f),
		fx.Populate(&c),
		fx.Populate(&s),
	)
	fmt.Printf("### done with app\n")
	if err := app.Err(); err != nil {
		return nil, err
	}
	go func() {
		err := f.Start()
		if err != nil {
			fmt.Printf("### error starting forwarder: %s\n", err)
		}
	}()
	return s, nil
}

type orchestratorinterfaceimpl struct {
	f defaultforwarder.Forwarder
}

func NewOrchestratorinterfaceimpl(f defaultforwarder.Forwarder) orchestratorinterface.Component {
	return &orchestratorinterfaceimpl{
		f: f,
	}
}

func (o *orchestratorinterfaceimpl) Get() (defaultforwarder.Forwarder, bool) {
	return o.f, true
}

func (o *orchestratorinterfaceimpl) Reset() {
	o.f = nil
}

func main() {
	set := component.TelemetrySettings{
		Logger: zap.NewNop(),
	}
	api := os.Getenv("DD_API_KEY")

	cfg := &Config{
		API: API{
			Site: "datadoghq.com",
			Key:  api,
		},
	}

	s, err := newSerializer(set, cfg)
	if err != nil {
		fmt.Printf("### error: %s\n", err)
	}
	fmt.Printf("### serializer: %v\n", s)
	err = s.SendIterableSeries(nil)
	if err != nil {
		fmt.Printf("### error: %s\n", err)
	}
}
