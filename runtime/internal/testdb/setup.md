
## 概述

```
mysql.Run()
    │
    ▼
┌─────────────────┐
│ MySQL Container │
│     RUNNING     │
└─────────────────┘
    │
    │ 测试执行
    ▼
defer
    │
    ▼
Terminate()
    │
    ├── stop
    └── remove
```

## 细节

对，理解基本完全正确。可以把这个方案理解成 **“一次性、无持久化、测试结束后清理干净”**。

### 1. `Terminate()` 后 Docker 容器没有残留

生命周期：

```text
mysql.Run()
    │
    ├── create container
    ├── start container
    └── wait MySQL ready
            │
            ▼
       测试执行
            │
            ▼
     Terminate()
            │
            ├── stop container
            │
            └── remove container
                    │
                    ▼
              Docker 容器消失
```

所以：

```bash
docker ps -a
```

正常情况下不会看到这个 Testcontainers 创建的 MySQL 容器。

对应的临时 container 也不会继续占用 Docker 的 container 元数据。

---

### 2. `"/var/lib/mysql"` 是放在内存中的

你配置了：

```go
testcontainers.WithTmpfs(map[string]string{
    "/var/lib/mysql": "rw,noexec,nosuid,size=1024m",
})
```

所以容器里面：

```text
/var/lib/mysql
        │
        ▼
      tmpfs
        │
        ▼
     RAM（内存）
```

也就是说 MySQL 的：

```text
/var/lib/mysql
├── ibdata*
├── ib_logfile*
├── #innodb_redo/
├── testdb/
└── ...
```

都在 tmpfs 中。

**不会作为 Docker volume 持久化到你的 Mac 磁盘。**

---

### 3. Terminate 后，这部分内存也一起消失

因此完整生命周期实际上是：

```text
Mac
 │
 └── Colima VM
       │
       └── Docker
            │
            └── MySQL Container
                  │
                  └── /var/lib/mysql
                        │
                        └── tmpfs → RAM
```

测试结束：

```text
Terminate()
   │
   ├── Container stop
   │
   ├── Container remove
   │
   └── tmpfs 一并释放
             │
             ▼
          内存释放
```

所以不会留下：

```text
❌ MySQL container
❌ MySQL volume
❌ /var/lib/mysql 数据目录
❌ MySQL 数据文件
```

---

### 4. 但有一个小细节要区分

严格来说：

> **没有 Docker 层面的持久化 MySQL 数据残留**

是的。

但是 Docker/Colima 自身可能存在一些正常的基础设施缓存，例如：

```text
mysql:8.4 image
```

如果 Testcontainers 拉取过：

```bash
docker images
```

你仍然可能看到：

```text
mysql    8.4    ...
```

这是**镜像**，不是 MySQL 数据。

下次：

```go
mysql.Run(ctx, "mysql:8.4", ...)
```

可以直接复用这个 image，不需要重新下载。

所以最终可以简单理解为：

```text
第一次运行
    │
    ├── pull mysql:8.4
    ├── create container
    ├── tmpfs → RAM
    └── test
          │
          ▼
      Terminate
          │
          ├── container 删除 ✅
          └── tmpfs 删除     ✅

Docker image
    │
    └── mysql:8.4 保留 ✅
```

**这其实非常适合你这种 Go Integration Test：MySQL 数据每次都是全新的，测试结束没有数据库数据残留；唯一长期保留的通常只是 `mysql:8.4` 镜像。**
