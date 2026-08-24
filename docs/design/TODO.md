

# TODO

## 系统字段允许忽略
当删除 of_* 表后，	
系统字段	在业务表上（deleted_at/version 列） 必须存在，

但也应该允许不存在，此时，采用设置，忽略类似的处理

## 自动 DDL schema

还要加上一条，提供根据 ODL 和 OBDA 的配置自动产生 DDL , 然后初始化数据库schema
