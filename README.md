# kubectl-tree

一个 kubectl 插件，用于把集群资源按“资源目录树”方式打印出来，便于从命名空间 / 节点 / Ingress 全链路视角排查与分析。

输出始终为 tree（目录树 + `|` 开头的表格行），不再提供 json/table 模式。

## 前置条件

- 本机已安装并可用：`kubectl`
- 本机可访问目标集群（使用你当前的 kubeconfig 规则，或通过 `--kubeconfig` 指定）

## 安装

确保编译产物在 PATH 中且名称为 `kubectl-tree`：

```bash
go build -o kubectl-tree .
sudo mv kubectl-tree /usr/local/bin/
```

之后即可通过 `kubectl tree ...` 调用。

## 用法

```bash
kubectl tree [flags]
```

### 视角（Scope）选择规则

kubectl-tree 有 3 种输出视角：`namespace` / `node` / `ingress`。

你可以显式指定：
- `--topo-scope=<scope>`
- 或使用等价简写：`--topo=<scope>`（内部会转换为 `--topo-scope`）

默认行为（不指定 `--topo-scope` / `--topo`）：
- 设置了 `--ingress` → 走 `ingress` 视角
- 设置了 `--node` → 走 `node` 视角（只看该节点上的 Pods）
- 否则 → 走 `namespace` 视角（当前 namespace 或 `-A`）

### 输出结构

- namespace 视角：`Namespace → Workload → ReplicaSet → Pods`
- node 视角：`Node → Namespace → Workload → ReplicaSet → Pods`（Pods 会先按节点过滤）
- ingress 视角：`Ingress → Service → Workload → ReplicaSet → Pods`

其中：
- Ingress/Service/Deployment/ReplicaSet/Pods 的表格列尽量对齐 kubectl 常见输出（`kubectl get ...` 的风格）
- 除 Deployment / ReplicaSet / Pods 这些层级展示表格列外，其它层级主要展示“结构关系”和必要的统计字段（例如 pods 数量）

### 示例

#### 命名空间视角（namespace → workload → ReplicaSet → Pods）

```bash
kubectl tree -A
```

只看单个命名空间：

```bash
kubectl tree -n default
```

包含 standalone pods（没有控制器的 Pod；默认会跳过）：

```bash
kubectl tree -A --include-standalone-pods
```

#### 节点视角（Node → Namespace → workload → ReplicaSet → Pods）

```bash
kubectl tree -A --node <nodeName>
```

只看某个命名空间：

```bash
kubectl tree -n default --node <nodeName>
```

显式指定 scope（等价于只写 `--node`）：

```bash
kubectl tree -A --topo=node --node <nodeName>
```

#### Ingress 全链路（Ingress → Service → Workload → ReplicaSet → Pods）

```bash
kubectl tree -n default --topo=ingress --ingress nginx
```

显式指定 scope 的另一种写法（与 `--topo=ingress` 等价）：

```bash
kubectl tree -n default --topo-scope=ingress --ingress nginx
```

### flags 说明

- `--kubeconfig <path>`
  - 指定 kubeconfig 路径（默认使用 kubectl 的加载规则）
- `-n, --namespace <ns>`
  - 指定命名空间（默认使用 kubeconfig 的当前 namespace）
  - 与 `-A/--all-namespaces` 不能同时使用
- `-A, --all-namespaces`
  - 所有命名空间
- `--node <nodeName>`
  - 指定节点名
  - 当 scope 为 `node` 时必填（否则会报错）
- `--ingress <name>`
  - 指定 Ingress 名称（当前代码按 `-n/--namespace` 指定的命名空间读取）
  - 当 scope 为 `ingress` 时必填（否则会报错）
- `--topo-scope node|namespace|ingress`
  - 显式指定输出视角
- `--topo=node|namespace|ingress`
  - `--topo-scope` 的等价写法
- `--include-standalone-pods`
  - 包含无控制器的 Pod（否则默认跳过）
- `--timeout <duration>`
  - 请求超时（默认 20s），示例：`--timeout 60s`