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
			Name:    "config",
			Usage:   "配置文件路径",
			Aliases: []string{"c"},
			Value:   "", // 默认为空字符串
			Sources: cli.EnvVars("FOUNDRY_CONFIG"),
		},
		&cli.BoolFlag{
			Name:  "verbose",
			Usage: "是否开启详细日志",
		},
	},
	Before: func(ctx context.Context, cmd *cli.Command) (context.Context, error) {
		configPath := cmd.Root().String("config")
		fmt.Println("configPath", configPath, len(configPath))
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
		return run(ctx, ":4000")
	},
	Commands: []*cli.Command{
		{
			Name:    "ddl",
			Usage:   "打印或执行 DDL 语句",
			Aliases: []string{"a"},
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "dialect",
					Usage: "打印用的 SQL 方言（mysql）。默认用配置的 DB_DRIVER",
				},
				&cli.StringFlag{
					Name:    "output",
					Usage:   "将 DDL 写到指定文件；省略则打印到 stdout",
					Aliases: []string{"o"},
				},
				&cli.BoolFlag{
					Name:    "execute",
					Usage:   "执行这些 DDL",
					Aliases: []string{"x"},
				},
				&cli.BoolFlag{
					Name:    "force",
					Usage:   "先删除这些表，然后重建",
					Aliases: []string{"f"},
				},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return ddl(cmd.String("dialect"), cmd.String("output"), cmd.Bool("execute"), cmd.Bool("force"))
			},
		},
		{
			Name:    "seed",
			Usage:   "将 domain pack 的 seed 数据写入数据库（幂等）",
			Aliases: []string{"s"},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return seed()
			},
		},
		{
			Name:    "run",
			Usage:   "启动 GraphQL 与 REST HTTP 服务",
			Aliases: []string{"t"},
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:  "addr",
					Usage: "监听地址",
					Value: ":4000",
					Sources: cli.NewValueSourceChain(
						cli.EnvVar("HTTP_ADDR"),
					),
				},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return run(ctx, cmd.String("addr"))
			},
		},
		{
			Name:    "sdl",
			Usage:   "打印 graphql schema",
			Aliases: []string{"g"},
			Flags: []cli.Flag{
				&cli.StringFlag{
					Name:    "output",
					Usage:   "将 GraphQL schema 写到指定文件；省略则打印到 stdout",
					Aliases: []string{"o"},
				},
			},
			Action: func(ctx context.Context, cmd *cli.Command) error {
				return sdl(cmd.String("output"))
			},
		},
	},
}

func main() {
	cmd.Run(context.Background(), os.Args)
}
