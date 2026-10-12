package tools

import (
	aisdk "github.com/grafana/ai-sdk"
	engineTools "github.com/openfoundry/agent/engine/tools"
)

func CreateTools(extra engineTools.ToolExtra) (aisdk.ToolSet, error) {
	// var CreateTools = func(extra engineTools.ToolExtra) (aisdk.ToolSet, error) {
	tools := aisdk.ToolSet{}
	gqlExec, err := CreateGqlExecTool(nil)
	if err != nil {
		return nil, err
	}
	tools["gql_exec"] = gqlExec
	readSdl, err := CreateReadSdlTool(nil)
	if err != nil {
		return nil, err
	}
	tools["read_sdl"] = readSdl

	return tools, nil
}
