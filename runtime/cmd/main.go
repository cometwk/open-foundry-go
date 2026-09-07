package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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

func ddl(dialect, output string, execute, force bool) error {
	stmts, err := bootstrap.MappedDDL(conf, dialect, force)
	if err != nil {
		slog.Error("print ddl failed", "error", err)
		return err
	}
	if err := writeDDL(stmts, output); err != nil {
		slog.Error("write ddl failed", "error", err, "output", output)
		return err
	}
	if !execute {
		return nil
	}
	if err := bootstrap.ExecStatements(conf, dialect, stmts); err != nil {
		slog.Error("exec ddl failed", "error", err)
		return err
	}
	return nil
}

func writeDDL(stmts []string, output string) error {
	rendered := make([]string, 0, len(stmts))
	for _, s := range stmts {
		s = withSQLSemi(s)
		if s == "" {
			continue
		}
		rendered = append(rendered, s)
	}
	if output == "" {
		for _, s := range rendered {
			fmt.Println(s)
		}
		return nil
	}
	path, err := resolveOutputPath(output)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	body := strings.Join(rendered, "\n")
	if body != "" {
		body += "\n"
	}
	return os.WriteFile(path, []byte(body), 0o644)
}

func withSQLSemi(s string) string {
	s = strings.TrimRight(s, " \t\r\n")
	if s == "" || strings.HasSuffix(s, ";") {
		return s
	}
	return s + ";"
}

func resolveOutputPath(output string) (string, error) {
	if filepath.IsAbs(output) {
		return output, nil
	}
	if wd, err := os.Getwd(); err == nil {
		if st, statErr := os.Stat(wd); statErr == nil && st.IsDir() {
			return filepath.Join(wd, output), nil
		}
	}
	if conf != nil && conf.BaseDir != "" {
		return filepath.Join(conf.BaseDir, output), nil
	}
	return filepath.Abs(output)
}
