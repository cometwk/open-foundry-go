/**
 * Agent配置 — 默认值来自 env
 */
package engine

import (
	"os"
)

type envConfig struct{}

var EnvConfig = envConfig{}

// AgentContextGit 是否包含 git 状态信息, 默认 on
func (e envConfig) AgentContextGit() bool {
	return os.Getenv("AGENT_CONTEXT_GIT") != "off"
}

// AgentTools 是否包含默认的工具集合, 默认 on
func (e envConfig) AgentTools() bool {
	return os.Getenv("AGENT_TOOLS") != "off"
}

// AgentMemory 是否开启记忆, 默认 off
func (e envConfig) AgentMemory() bool {
	return os.Getenv("AGENT_MEMORY") == "on"
}
