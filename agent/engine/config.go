/**
 * Agent配置 — 默认值来自 env
 */
package engine

import (
	"github.com/openfoundry/lib/env"
)

type envConfig struct{}

var EnvConfig = envConfig{}

// AgentContextGit 是否包含 git 状态信息, 默认 off
func (e envConfig) AgentContextGit() bool {
	return env.String("AGENT_CONTEXT_GIT", "off") == "on"
}

// AgentTools 是否包含默认的工具集合, 默认 on
func (e envConfig) AgentTools() bool {
	return env.String("AGENT_TOOLS", "on") == "on"
}

// AgentMemory 是否开启记忆, 默认 off
func (e envConfig) AgentMemory() bool {
	return env.String("AGENT_CONTEXT_GIT", "off") == "on"
}
