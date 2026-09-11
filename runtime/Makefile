# 自动加载上一级目录的 .env
-include ../.env
export

# 定义测试包路径
MYSQL_PACKAGES := ./storage/mysqlobda/... ./obda/dialect/mysql/...

.PHONY: all test-mysql

all:run

run:
	cd ./cmd && go run . run

# 执行 MySQL 模块测试
test-mysql:
	@echo "==> Running MySQL OBDA tests..."
	@go test $(MYSQL_PACKAGES) -v 2>&1 | tail -250
