package llm

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sync"

	"github.com/grafana/ai-sdk/provider"
	openaiCompatible "github.com/grafana/ai-sdk/providers/openai-compatible"
	"github.com/openfoundry/lib/env"
	"gopkg.in/yaml.v3"
)

type modelFile struct {
	Providers []providerConf       `yaml:"providers"`
	Models    map[string]modelConf `yaml:"models"`
}

type providerConf struct {
	ID           string `yaml:"id"`
	Name         string `yaml:"name"`
	ProviderType string `yaml:"provider_type"`
	APIBase      string `yaml:"api_base"`
	APIKey       string `yaml:"api_key"`
}

type modelConf struct {
	ProviderID string `yaml:"provider_id"`
	ModelName  string `yaml:"model_name"`
}

var (
	loadOnce sync.Once
	loaded   *modelFile
	loadErr  error
	envRef   = regexp.MustCompile(`\$\{([^}:]+)(?::([^}]*))?\}`)
)

func NewDefaultModel() provider.LanguageModel {
	return mustLanguageModel("default")
}

// New 按别名构建模型 (model.yaml 中的 default/flash/object)，空别名等同 "default"
func New(alias string) (provider.LanguageModel, error) {
	if alias == "" {
		alias = "default"
	}
	return newLanguageModel(alias)
}

func NewFlashModel() provider.LanguageModel {
	return mustLanguageModel("flash")
}

func NewObjectModel() provider.LanguageModel {
	return mustLanguageModel("object")
}

func mustLanguageModel(alias string) provider.LanguageModel {
	m, err := newLanguageModel(alias)
	if err != nil {
		panic(err)
	}
	return m
}

func newLanguageModel(alias string) (provider.LanguageModel, error) {
	cfg, err := loadModelFile()
	if err != nil {
		return nil, err
	}
	mc, ok := cfg.Models[alias]
	if !ok {
		return nil, fmt.Errorf("model.yaml: unknown model %q", alias)
	}
	pc, ok := cfg.provider(mc.ProviderID)
	if !ok {
		return nil, fmt.Errorf("model.yaml: model %q references unknown provider %q", alias, mc.ProviderID)
	}

	switch pc.ProviderType {
	case "openai_compatible":
		return openaiCompatible.New(
			mc.ModelName,
			openaiCompatible.WithAPIKey(expandEnv(pc.APIKey)),
			openaiCompatible.WithBaseURL(pc.APIBase),
			openaiCompatible.WithStructuredOutputs(true),
		), nil
	default:
		return nil, fmt.Errorf("model.yaml: unsupported provider_type %q", pc.ProviderType)
	}
}

func (c *modelFile) provider(id string) (providerConf, bool) {
	for _, p := range c.Providers {
		if p.ID == id {
			return p, true
		}
	}
	return providerConf{}, false
}

func loadModelFile() (*modelFile, error) {
	loadOnce.Do(func() {
		path := filepath.Join(env.BaseDir(), "model.yaml")
		raw, err := os.ReadFile(path)
		if err != nil {
			loadErr = fmt.Errorf("read %s: %w", path, err)
			return
		}
		var cfg modelFile
		if err := yaml.Unmarshal(raw, &cfg); err != nil {
			loadErr = fmt.Errorf("parse %s: %w", path, err)
			return
		}
		loaded = &cfg
	})
	return loaded, loadErr
}

func expandEnv(s string) string {
	return envRef.ReplaceAllStringFunc(s, func(m string) string {
		parts := envRef.FindStringSubmatch(m)
		if v := os.Getenv(parts[1]); v != "" {
			return v
		}
		return parts[2]
	})
}
