# kubectl-tree

[中文](#中文) | [English](#english)

## 中文

一个 kubectl 插件，用于把集群资源按“资源目录树”方式打印出来，便于从命名空间 / 节点 / Ingress 全链路视角排查与分析。

输出始终为 tree（目录树 + `|` 开头的表格行），不提供 json/table 模式。

### 前置条件

- 本机已安装并可用：`kubectl`
- 本机可访问目标集群（使用你当前的 kubeconfig 规则，或通过 `--kubeconfig` 指定）

### 安装

确保可执行文件在 PATH 中且名称为 `kubectl-tree`（kubectl plugin 发现规则）：

```bash
make build
sudo install -m 0755 build/kubectl-tree /usr/local/bin/kubectl-tree
```

或直接用 Go 编译：

```bash
go build -o kubectl-tree .
sudo install -m 0755 kubectl-tree /usr/local/bin/kubectl-tree
```

之后即可通过 `kubectl tree ...` 调用。

### 用法

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

### Flags 说明

- `--kubeconfig <path>`：指定 kubeconfig 路径（默认使用 kubectl 的加载规则）
- `-n, --namespace <ns>`：指定命名空间（默认使用 kubeconfig 的当前 namespace；与 `-A/--all-namespaces` 不能同时使用）
- `-A, --all-namespaces`：所有命名空间
- `--node <nodeName>`：指定节点名（scope 为 `node` 时必填）
- `--ingress <name>`：指定 Ingress 名称（当前实现按 `-n/--namespace` 指定的命名空间读取；scope 为 `ingress` 时必填）
- `--topo-scope node|namespace|ingress`：显式指定输出视角
- `--topo=node|namespace|ingress`：`--topo-scope` 的等价写法
- `--include-standalone-pods`：包含无控制器的 Pod（否则默认跳过）
- `--timeout <duration>`：请求超时（默认 20s），示例：`--timeout 60s`

## English

A kubectl plugin that prints Kubernetes resources as a topology tree, making it easier to troubleshoot from a namespace / node / ingress end-to-end view.

The output is always a tree (directory-like branches + `|`-prefixed table rows). No json/table modes are provided.

### Prerequisites

- `kubectl` installed and working
- Access to the target cluster (using your current kubeconfig loading rules, or via `--kubeconfig`)

### Install

Make sure the binary is in your PATH and named `kubectl-tree` (kubectl plugin discovery rule):

```bash
make build
sudo install -m 0755 build/kubectl-tree /usr/local/bin/kubectl-tree
```

Or build with Go directly:

```bash
go build -o kubectl-tree .
sudo install -m 0755 kubectl-tree /usr/local/bin/kubectl-tree
```

Then you can run it as `kubectl tree ...`.

### Usage

```bash
kubectl tree [flags]
```

### Scope Selection

kubectl-tree supports 3 scopes: `namespace` / `node` / `ingress`.

You can set it explicitly:
- `--topo-scope=<scope>`
- or the shorthand: `--topo=<scope>` (internally converted to `--topo-scope`)

Default behavior (when `--topo-scope` / `--topo` is not set):
- If `--ingress` is set → use `ingress` scope
- Else if `--node` is set → use `node` scope (pods are filtered by node)
- Otherwise → use `namespace` scope (current namespace or `-A`)

### Output Structure

- namespace scope: `Namespace → Workload → ReplicaSet → Pods`
- node scope: `Node → Namespace → Workload → ReplicaSet → Pods` (pods are filtered by node first)
- ingress scope: `Ingress → Service → Workload → ReplicaSet → Pods`

Notes:
- Table columns for Ingress/Service/Deployment/ReplicaSet/Pods are aligned with typical `kubectl get ...` output style
- Only Deployment / ReplicaSet / Pods levels show detailed tables; other levels focus on structure and minimal stats (e.g. pod counts)

### Examples

#### Namespace Scope (namespace → workload → ReplicaSet → Pods)

```bash
kubectl tree -A
```

Single namespace:

```bash
kubectl tree -n default
```

Include standalone pods (pods without controllers; skipped by default):

```bash
kubectl tree -A --include-standalone-pods
```

#### Node Scope (Node → Namespace → workload → ReplicaSet → Pods)

```bash
kubectl tree -A --node <nodeName>
```

Single namespace:

```bash
kubectl tree -n default --node <nodeName>
```

Explicit scope (equivalent to providing `--node` only):

```bash
kubectl tree -A --topo=node --node <nodeName>
```

#### Ingress End-to-End (Ingress → Service → Workload → ReplicaSet → Pods)

```bash
kubectl tree -n default --topo=ingress --ingress nginx
```

Alternative explicit scope syntax (same as `--topo=ingress`):

```bash
kubectl tree -n default --topo-scope=ingress --ingress nginx
```

### Flags

- `--kubeconfig <path>`: kubeconfig path (defaults to kubectl loading rules)
- `-n, --namespace <ns>`: namespace (defaults to current namespace in kubeconfig; cannot be used with `-A/--all-namespaces`)
- `-A, --all-namespaces`: all namespaces
- `--node <nodeName>`: node name (required when scope is `node`)
- `--ingress <name>`: ingress name (looked up in the namespace from `-n/--namespace`; required when scope is `ingress`)
- `--topo-scope node|namespace|ingress`: set scope explicitly
- `--topo=node|namespace|ingress`: shorthand for `--topo-scope`
- `--include-standalone-pods`: include pods without controllers (skipped by default)
- `--timeout <duration>`: request timeout (default: 20s), e.g. `--timeout 60s`
