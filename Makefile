# 自动加载上一级目录的 .env
-include ./.env
export

.PHONY: all run

all:run

run:
	cd runtime/cmd && go run . run
