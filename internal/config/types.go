package config

import (
	"time"
)

type ParameterType string

const (
	ParameterTypeString  ParameterType = "string"
	ParameterTypeNumber  ParameterType = "number"
	ParameterTypeInteger ParameterType = "integer"
	ParameterTypeBoolean ParameterType = "boolean"
)

type Configuration struct {
	Tools map[string]ToolDefinition `yaml:"tools"`
}

type ToolDefinition struct {
	Description string               `yaml:"description"`
	Parameters  map[string]Parameter `yaml:"parameters,omitempty"`
	Container   Container            `yaml:"container"`
}

type Container struct {
	Image   string            `yaml:"image"`
	Command string            `yaml:"command,omitempty"`
	Args    []string          `yaml:"args,omitempty"`
	Env     map[string]string `yaml:"env,omitempty"`
	// Network specifies the network mode, e.g. 'host' or 'none'
	Network string        `yaml:"network,omitempty"`
	Timeout time.Duration `yaml:"timeout,omitempty"`
}

type Parameter struct {
	Description string        `yaml:"description"`
	Type        ParameterType `yaml:"type,omitempty"`
	MinValue    *float64      `yaml:"minValue,omitempty"`
	MaxValue    *float64      `yaml:"maxValue,omitempty"`
	Required    *bool         `yaml:"required,omitempty"`
}
