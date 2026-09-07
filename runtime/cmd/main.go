package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"github.com/openfoundry/runtime/bootstrap"
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
			Usage:   "打印或执行 DDL 语句",
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "dialect",
					Usage: "打印用的 SQL 方言（mysql 或 sqlite）。默认用配置的 DB_DRIVER",
				},
				&cli.StringFlag{
					Name:    "output",
					Aliases: []string{"o"},
					Usage:   "将 DDL 写到指定文件；省略则打印到 stdout",
				},
				&cli.BoolFlag{
					Name:    "execute",
					Aliases: []string{"x"},
					Usage:   "执行这些 DDL",
				},
				&cli.BoolFlag{
					Name:    "force",
					Aliases: []string{"f"},
					Usage:   "先删除这些表，然后重建",
				},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return ddl(cmd.String("dialect"), cmd.String("output"), cmd.Bool("execute"), cmd.Bool("force"))
			},
		},
		{
			Name:    "seed",
			Aliases: []string{"s"},
			Usage:   "将 domain pack 的 seed 数据写入数据库（幂等）",
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return seed()
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
