package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Defaults Defaults       `yaml:"defaults"`
	Runners  []string       `yaml:"runners"`
	AiPerf   AiPerfConfig   `yaml:"aiperf"`
	VLMBench VLMBenchConfig `yaml:"vlmbench"`
	Output   Output         `yaml:"output"`
}

type Defaults struct {
	Model        string `yaml:"model"`
	OutputTokens int    `yaml:"output_tokens"`
}

type AiPerfConfig struct {
	URL          string `yaml:"url"`
	EndpointType string `yaml:"endpoint_type"` // chat | completions | embeddings
	Endpoint     string `yaml:"endpoint"`      // e.g. /v1/chat/completions
	Streaming    bool   `yaml:"streaming"`
	RequestRate  int    `yaml:"request_rate"`  // Poisson req/s
	Concurrency  int    `yaml:"concurrency"`   // fixed workers; takes priority over request_rate
	RequestCount int    `yaml:"request_count"`
}

type VLMBenchConfig struct {
	URL            string `yaml:"url"`
	Dataset        string `yaml:"dataset"`          // hf://org/dataset
	DatasetTextCol string `yaml:"dataset_text_col"` // text column for text-only benchmarks
	DatasetSplit   string `yaml:"dataset_split"`    // default: train
	Input          string `yaml:"input"`            // local file/dir (images, PDFs, video)
	Prompt         string `yaml:"prompt"`           // set "" to use text col as full message
	Backend        string `yaml:"backend"`          // auto | ollama | vllm | vllm-openai:latest
	Runs           int    `yaml:"runs"`
	Concurrency    string `yaml:"concurrency"` // single value or sweep e.g. "4,8,16"
	MaxSamples     int    `yaml:"max_samples"`
}

type Output struct {
	Format string `yaml:"format"` // table | json
	File   string `yaml:"file"`
}

func FromFile(path string) (*Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var cfg Config
	if err := yaml.NewDecoder(f).Decode(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
