package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/openfoundry/runtime/bootstrap"
	"github.com/openfoundry/runtime/obda"
	"github.com/openfoundry/runtime/obda/dialect/sqlite"
	"github.com/urfave/cli/v3"
)

var conf *bootstrap.Conf
var cmd = &cli.Command{
	Name:  "foundry",
	Usage: "Open Ontology Foundry",
	Flags: []cli.Flag{ // 全局 Flag
		&cli.StringFlag{
			Name:  "config",
			Usage: "配置文件路径",
			Sources: cli.NewValueSourceChain(
				cli.EnvVar("FOUNDRY_CONFIG"),
			),
		},
		&cli.BoolFlag{
			Name:  "verbose",
			Usage: "是否开启详细日志",
		},
	},
	Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
		configPath := cmd.Root().String("config")
		var err error
		conf, err = bootstrap.LoadConfig(configPath)
		if err != nil {
			slog.Error("load config file failed", "error", err, "configPath", configPath)
			return ctx, err
		}
		return ctx, nil
	},
	// 3. 根命令自身的 Action（仅当只敲 `./myapp` 且不加任何子命令时触发）
	Action: func(ctx context.Context, cmd *cli.Command) error {
		// fmt.Println("[根命令 Action] 未指定子命令，打印默认提示信息...")
		// return cli.ShowAppHelp(cmd)
		return run()
	},
	Commands: []*cli.Command{
		{
			Name:    "ddl",
			Aliases: []string{"a"},
			Usage:   "打印DDL语句",
			Action: func(ctx context.Context, cmd *cli.Command) error {
				slog.Info("config: ", "domainPacks", conf.DomainPacks)
				return ddl()
			},
		},
		{
			Name:    "complete",
			Aliases: []string{"c"},
			Usage:   "complete a task on the list",
			Action: func(ctx context.Context, cmd *cli.Command) error {
				fmt.Println("completed task: ", cmd.Args().First())
				return nil
			},
		},
		{
			Name:    "template",
			Aliases: []string{"t"},
			Usage:   "options for task templates",
			Commands: []*cli.Command{
				{
					Name:  "add",
					Usage: "add a new template",
					Action: func(ctx context.Context, cmd *cli.Command) error {
						fmt.Println("new task template: ", cmd.Args().First())
						return nil
					},
				},
				{
					Name:  "remove",
					Usage: "remove an existing template",
					Action: func(ctx context.Context, cmd *cli.Command) error {
						fmt.Println("removed task template: ", cmd.Args().First())
						return nil
					},
				},
			},
		},
	},
}

func main() {
	cmd.Run(context.Background(), os.Args)
}

func run() error {
	slog.Info("Running...")
	return nil
}

func ddl() error {
	b, err := bootstrap.Open(conf)
	if err != nil {
		slog.Error("open bootstrap failed", "error", err)
		return err
	}
	for _, m := range b.Mappings {
		compiled, err := obda.Compile(b.Schema, m.Doc)
		if err != nil {
			return err
		}
		stmts, err := sqlite.MappedTableStatements(compiled)
		if err != nil {
			return err
		}
		for _, s := range stmts {
			fmt.Println(s)
		}
	}
	return nil
}
