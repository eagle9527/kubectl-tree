package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	appsv1 "k8s.io/api/apps/v1"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	networkingv1 "k8s.io/api/networking/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/duration"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type options struct {
	kubeconfig            string
	namespace             string
	allNamespaces         bool
	nodeName              string
	ingressName           string
	topologyScope         string
	includeStandalonePods bool
	timeout               time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.kubeconfig, "kubeconfig", "", "")
	flag.StringVar(&o.namespace, "n", "", "")
	flag.StringVar(&o.namespace, "namespace", "", "")
	flag.BoolVar(&o.allNamespaces, "A", false, "")
	flag.BoolVar(&o.allNamespaces, "all-namespaces", false, "")
	flag.StringVar(&o.nodeName, "node", "", "")
	flag.StringVar(&o.ingressName, "ingress", "", "")
	flag.StringVar(&o.topologyScope, "topo-scope", "", "")
	flag.BoolVar(&o.includeStandalonePods, "include-standalone-pods", false, "")
	flag.DurationVar(&o.timeout, "timeout", 20*time.Second, "")
	_ = flag.CommandLine.Parse(preprocessArgs(os.Args)[1:])

	if err := run(o); err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(1)
	}
}

func preprocessArgs(args []string) []string {
	if len(args) == 0 {
		return args
	}
	out := []string{args[0]}
	for i := 1; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "--topo=") {
			scope := strings.TrimPrefix(a, "--topo=")
			out = append(out, "--topo-scope="+scope)
			continue
		}
		if strings.HasPrefix(a, "--topology=") {
			scope := strings.TrimPrefix(a, "--topology=")
			out = append(out, "--topo-scope="+scope)
			continue
		}
		out = append(out, a)
	}
	return out
}

func run(o options) error {
	ctx, cancel := context.WithTimeout(context.Background(), o.timeout)
	defer cancel()

	clientset, currentNS, err := buildClientset(o.kubeconfig)
	if err != nil {
		return err
	}

	ns, allNS, err := resolveNamespace(o.namespace, o.allNamespaces, currentNS)
	if err != nil {
		return err
	}

	scope := strings.TrimSpace(strings.ToLower(o.topologyScope))
	if scope == "" {
		if o.ingressName != "" {
			scope = "ingress"
		} else if o.nodeName != "" {
			scope = "node"
		} else {
			scope = "namespace"
		}
	}

	switch scope {
	case "node":
		if o.nodeName == "" {
			return errors.New("--topo-scope=node requires --node")
		}
		fmt.Println("Node")
		nw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(nw, "| NAME\tPODS")
		opts := metav1.ListOptions{FieldSelector: "spec.nodeName=" + o.nodeName}
		rows, err := collectNamespaceTopologyWithListOptions(ctx, clientset, ns, opts, allNS, o.includeStandalonePods)
		if err != nil {
			return err
		}
		podsTotal := 0
		for i := range rows {
			podsTotal += namespacePodCount(rows[i])
		}
		fmt.Fprintf(nw, "| %s\t%d\n", o.nodeName, podsTotal)
		_ = nw.Flush()
		sortNamespaceTopology(rows)
		printNamespaceTopology(rows)
		return nil
	case "namespace", "ns":
		rows, err := collectNamespaceTopologyWithListOptions(ctx, clientset, ns, metav1.ListOptions{}, allNS, o.includeStandalonePods)
		if err != nil {
			return err
		}
		sortNamespaceTopology(rows)
		printNamespaceTopology(rows)
		return nil
	case "ingress":
		if o.ingressName == "" {
			return errors.New("--topo=ingress requires --ingress <name>")
		}
		rows, err := collectIngressTopology(ctx, clientset, ns, allNS, o.ingressName, o.includeStandalonePods)
		if err != nil {
			return err
		}
		sortIngressTopology(rows)
		printIngressTopology(rows)
		return nil
	default:
		return fmt.Errorf("invalid --topo-scope: %s (valid: node,namespace,ingress)", o.topologyScope)
	}
}

func resolveNamespace(ns string, all bool, currentNS string) (string, bool, error) {
	if all && ns != "" {
		return "", false, errors.New("cannot set both --all-namespaces and --namespace")
	}
	if all {
		return "", true, nil
	}
	if ns != "" {
		return ns, false, nil
	}
	if currentNS == "" {
		currentNS = "default"
	}
	return currentNS, false, nil
}

type namespaceTopo struct {
	Namespace string
	workloads []workloadTopo
}

type workloadTopo struct {
	Kind      string
	Name      string
	mids      []midTopo
	Ready     string
	UpToDate  int32
	Available int32
	Age       string
}

type midTopo struct {
	Kind    string
	Name    string
	pods    []podTopoItem
	Desired int32
	Current int32
	Ready   int32
	Age     string
}

type podTopoItem struct {
	Name     string
	NodeName string
	Ready    string
	Status   string
	PodIP    string
	Restarts int32
	Age      string
}

type ingressTopo struct {
	Namespace string
	Name      string
	Class     string
	Hosts     string
	Address   string
	Ports     string
	Age       string
	services  []serviceTopo
}

type serviceTopo struct {
	Name       string
	Port       string
	Type       string
	ClusterIP  string
	ExternalIP string
	Ports      string
	Age        string
	workloads  []workloadTopo
}

type serviceRef struct {
	name string
	port string
}

func collectNamespaceTopology(ctx context.Context, clientset *kubernetes.Clientset, ns string, allNS bool, includeStandalone bool) ([]namespaceTopo, error) {
	return collectNamespaceTopologyWithListOptions(ctx, clientset, ns, metav1.ListOptions{}, allNS, includeStandalone)
}

func collectNamespaceTopologyWithListOptions(ctx context.Context, clientset *kubernetes.Clientset, ns string, listOpts metav1.ListOptions, allNS bool, includeStandalone bool) ([]namespaceTopo, error) {
	list, err := clientset.CoreV1().Pods(ns).List(ctx, listOpts)
	if err != nil {
		return nil, err
	}

	rsCache := map[string]*appsv1.ReplicaSet{}
	jobCache := map[string]*batchv1.Job{}
	deployCache := map[string]*appsv1.Deployment{}

	nsMap := map[string]*namespaceTopo{}
	wlMap := map[string]map[string]*workloadTopo{}
	midMap := map[string]map[string]map[string]*midTopo{}

	for _, p := range list.Items {
		chain, ok, err := resolveOwnerChain(ctx, clientset, p.Namespace, &p, rsCache, jobCache)
		if err != nil {
			return nil, err
		}
		if !ok {
			if !includeStandalone {
				continue
			}
			chain = ownerChain{topKind: "standalone", topName: "-"}
		}

		ready, status, node, ip, restarts, age := podListFields(p)

		nt := nsMap[p.Namespace]
		if nt == nil {
			nt = &namespaceTopo{Namespace: p.Namespace}
			nsMap[p.Namespace] = nt
		}
		ownerKey := chain.topKind + "/" + chain.topName
		if wlMap[p.Namespace] == nil {
			wlMap[p.Namespace] = map[string]*workloadTopo{}
		}
		wt := wlMap[p.Namespace][ownerKey]
		if wt == nil {
			wt = &workloadTopo{Kind: chain.topKind, Name: chain.topName}
			wlMap[p.Namespace][ownerKey] = wt
			if strings.ToLower(chain.topKind) == "deployment" {
				dep, _ := getDeployment(ctx, clientset, p.Namespace, chain.topName, deployCache)
				if dep != nil {
					replicas := int32(1)
					if dep.Spec.Replicas != nil {
						replicas = *dep.Spec.Replicas
					}
					wt.Ready = fmt.Sprintf("%d/%d", dep.Status.ReadyReplicas, replicas)
					wt.UpToDate = dep.Status.UpdatedReplicas
					wt.Available = dep.Status.AvailableReplicas
					wt.Age = ageString(dep.CreationTimestamp)
				}
			}
		}

		if midMap[p.Namespace] == nil {
			midMap[p.Namespace] = map[string]map[string]*midTopo{}
		}
		if midMap[p.Namespace][ownerKey] == nil {
			midMap[p.Namespace][ownerKey] = map[string]*midTopo{}
		}
		midKey := chain.midKind + "/" + chain.midName
		mt := midMap[p.Namespace][ownerKey][midKey]
		if mt == nil {
			mt = &midTopo{Kind: chain.midKind, Name: chain.midName}
			midMap[p.Namespace][ownerKey][midKey] = mt
			if strings.ToLower(chain.midKind) == "replicaset" && chain.midName != "" {
				rs, _ := getReplicaSet(ctx, clientset, p.Namespace, chain.midName, rsCache)
				if rs != nil {
					desired := int32(1)
					if rs.Spec.Replicas != nil {
						desired = *rs.Spec.Replicas
					}
					mt.Desired = desired
					mt.Current = rs.Status.Replicas
					mt.Ready = rs.Status.ReadyReplicas
					mt.Age = ageString(rs.CreationTimestamp)
				}
			}
		}
		mt.pods = append(mt.pods, podTopoItem{Name: p.Name, NodeName: node, Ready: ready, Status: status, PodIP: ip, Restarts: restarts, Age: age})
	}

	out := make([]namespaceTopo, 0, len(nsMap))
	for namespace, nt := range nsMap {
		wls := wlMap[namespace]
		nt.workloads = make([]workloadTopo, 0, len(wls))
		for ownerKey, wt := range wls {
			mids := midMap[namespace][ownerKey]
			wt.mids = make([]midTopo, 0, len(mids))
			for _, mt := range mids {
				wt.mids = append(wt.mids, *mt)
			}
			nt.workloads = append(nt.workloads, *wt)
		}
		out = append(out, *nt)
	}
	_ = allNS
	return out, nil
}

func collectIngressTopology(ctx context.Context, clientset *kubernetes.Clientset, ns string, allNS bool, ingressName string, includeStandalone bool) ([]ingressTopo, error) {
	ing, err := clientset.NetworkingV1().Ingresses(ns).Get(ctx, ingressName, metav1.GetOptions{})
	if err != nil {
		return nil, err
	}

	refs := ingressServiceRefs(ing)
	seen := map[string]struct{}{}
	unique := make([]serviceRef, 0, len(refs))
	for _, r := range refs {
		k := r.name + ":" + r.port
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		unique = append(unique, r)
	}

	rsCache := map[string]*appsv1.ReplicaSet{}
	jobCache := map[string]*batchv1.Job{}

	out := ingressTopo{
		Namespace: ing.Namespace,
		Name:      ing.Name,
		Class:     ingressClass(ing),
		Hosts:     ingressHosts(ing),
		Address:   ingressAddress(ing),
		Ports:     ingressPorts(ing),
		Age:       ageString(ing.CreationTimestamp),
	}
	for _, ref := range unique {
		svc, err := clientset.CoreV1().Services(ing.Namespace).Get(ctx, ref.name, metav1.GetOptions{})
		if err != nil {
			return nil, err
		}

		selector := ""
		if len(svc.Spec.Selector) > 0 {
			selector = labels.SelectorFromSet(labels.Set(svc.Spec.Selector)).String()
		}
		st := serviceTopo{
			Name:       svc.Name,
			Port:       ref.port,
			Type:       string(svc.Spec.Type),
			ClusterIP:  svc.Spec.ClusterIP,
			ExternalIP: serviceExternalIP(svc),
			Ports:      servicePorts(svc),
			Age:        ageString(svc.CreationTimestamp),
		}
		if len(svc.Spec.Selector) == 0 {
			out.services = append(out.services, st)
			continue
		}

		podList, err := clientset.CoreV1().Pods(ing.Namespace).List(ctx, metav1.ListOptions{LabelSelector: selector})
		if err != nil {
			return nil, err
		}

		wls, err := buildWorkloadTopoForPods(ctx, clientset, ing.Namespace, podList.Items, includeStandalone, rsCache, jobCache)
		if err != nil {
			return nil, err
		}
		st.workloads = wls
		out.services = append(out.services, st)
	}

	_ = allNS
	return []ingressTopo{out}, nil
}

func ingressServiceRefs(ing *networkingv1.Ingress) []serviceRef {
	var out []serviceRef
	if ing.Spec.DefaultBackend != nil && ing.Spec.DefaultBackend.Service != nil {
		out = append(out, serviceRef{name: ing.Spec.DefaultBackend.Service.Name, port: ingressPortString(ing.Spec.DefaultBackend.Service.Port)})
	}
	for i := range ing.Spec.Rules {
		r := ing.Spec.Rules[i]
		if r.HTTP == nil {
			continue
		}
		for j := range r.HTTP.Paths {
			p := r.HTTP.Paths[j]
			if p.Backend.Service == nil {
				continue
			}
			out = append(out, serviceRef{name: p.Backend.Service.Name, port: ingressPortString(p.Backend.Service.Port)})
		}
	}
	return out
}

func ingressPortString(p networkingv1.ServiceBackendPort) string {
	if p.Number != 0 {
		return fmt.Sprintf("%d", p.Number)
	}
	if p.Name != "" {
		return p.Name
	}
	return "-"
}

func ingressHosts(ing *networkingv1.Ingress) string {
	seen := map[string]struct{}{}
	var hosts []string
	for i := range ing.Spec.Rules {
		h := strings.TrimSpace(ing.Spec.Rules[i].Host)
		if h == "" {
			h = "*"
		}
		if _, ok := seen[h]; ok {
			continue
		}
		seen[h] = struct{}{}
		hosts = append(hosts, h)
	}
	if len(hosts) == 0 {
		return "*"
	}
	sort.Strings(hosts)
	return strings.Join(hosts, ",")
}

func ingressAddress(ing *networkingv1.Ingress) string {
	var out []string
	for i := range ing.Status.LoadBalancer.Ingress {
		it := ing.Status.LoadBalancer.Ingress[i]
		if it.IP != "" {
			out = append(out, it.IP)
			continue
		}
		if it.Hostname != "" {
			out = append(out, it.Hostname)
		}
	}
	if len(out) == 0 {
		return "-"
	}
	return strings.Join(out, ",")
}

func ingressPorts(ing *networkingv1.Ingress) string {
	ports := []string{}
	has80 := false
	if ing.Spec.DefaultBackend != nil {
		has80 = true
	}
	for i := range ing.Spec.Rules {
		if ing.Spec.Rules[i].HTTP != nil {
			has80 = true
			break
		}
	}
	if has80 {
		ports = append(ports, "80")
	}
	if len(ing.Spec.TLS) > 0 {
		ports = append(ports, "443")
	}
	if len(ports) == 0 {
		return "-"
	}
	return strings.Join(ports, ",")
}

func ingressClass(ing *networkingv1.Ingress) string {
	if ing.Spec.IngressClassName != nil && strings.TrimSpace(*ing.Spec.IngressClassName) != "" {
		return *ing.Spec.IngressClassName
	}
	if v, ok := ing.Annotations["kubernetes.io/ingress.class"]; ok && strings.TrimSpace(v) != "" {
		return v
	}
	return "<none>"
}

func serviceExternalIP(svc *corev1.Service) string {
	var out []string
	out = append(out, svc.Spec.ExternalIPs...)
	for i := range svc.Status.LoadBalancer.Ingress {
		it := svc.Status.LoadBalancer.Ingress[i]
		if it.IP != "" {
			out = append(out, it.IP)
			continue
		}
		if it.Hostname != "" {
			out = append(out, it.Hostname)
		}
	}
	if len(out) == 0 {
		return "<none>"
	}
	return strings.Join(out, ",")
}

func servicePorts(svc *corev1.Service) string {
	var out []string
	for i := range svc.Spec.Ports {
		p := svc.Spec.Ports[i]
		if p.NodePort != 0 {
			out = append(out, fmt.Sprintf("%d:%d/%s", p.Port, p.NodePort, p.Protocol))
			continue
		}
		out = append(out, fmt.Sprintf("%d/%s", p.Port, p.Protocol))
	}
	if len(out) == 0 {
		return "-"
	}
	return strings.Join(out, ",")
}

func ageString(ts metav1.Time) string {
	if ts.IsZero() {
		return "-"
	}
	d := time.Since(ts.Time)
	if d < 0 {
		d = -d
	}
	return duration.HumanDuration(d)
}

func buildWorkloadTopoForPods(ctx context.Context, clientset *kubernetes.Clientset, ns string, pods []corev1.Pod, includeStandalone bool, rsCache map[string]*appsv1.ReplicaSet, jobCache map[string]*batchv1.Job) ([]workloadTopo, error) {
	wlMap := map[string]*workloadTopo{}
	midMap := map[string]map[string]*midTopo{}
	deployCache := map[string]*appsv1.Deployment{}

	for i := range pods {
		p := pods[i]
		chain, ok, err := resolveOwnerChain(ctx, clientset, ns, &p, rsCache, jobCache)
		if err != nil {
			return nil, err
		}
		if !ok {
			if !includeStandalone {
				continue
			}
			chain = ownerChain{topKind: "standalone", topName: "-", midKind: "", midName: ""}
		}

		ready, status, node, ip, restarts, age := podListFields(p)

		ownerKey := chain.topKind + "/" + chain.topName
		wt := wlMap[ownerKey]
		if wt == nil {
			wt = &workloadTopo{Kind: chain.topKind, Name: chain.topName}
			wlMap[ownerKey] = wt
			if strings.ToLower(chain.topKind) == "deployment" {
				dep, _ := getDeployment(ctx, clientset, ns, chain.topName, deployCache)
				if dep != nil {
					replicas := int32(1)
					if dep.Spec.Replicas != nil {
						replicas = *dep.Spec.Replicas
					}
					wt.Ready = fmt.Sprintf("%d/%d", dep.Status.ReadyReplicas, replicas)
					wt.UpToDate = dep.Status.UpdatedReplicas
					wt.Available = dep.Status.AvailableReplicas
					wt.Age = ageString(dep.CreationTimestamp)
				}
			}
		}

		if midMap[ownerKey] == nil {
			midMap[ownerKey] = map[string]*midTopo{}
		}
		midKey := chain.midKind + "/" + chain.midName
		mt := midMap[ownerKey][midKey]
		if mt == nil {
			mt = &midTopo{Kind: chain.midKind, Name: chain.midName}
			midMap[ownerKey][midKey] = mt
			if strings.ToLower(chain.midKind) == "replicaset" && chain.midName != "" {
				rs, _ := getReplicaSet(ctx, clientset, ns, chain.midName, rsCache)
				if rs != nil {
					desired := int32(1)
					if rs.Spec.Replicas != nil {
						desired = *rs.Spec.Replicas
					}
					mt.Desired = desired
					mt.Current = rs.Status.Replicas
					mt.Ready = rs.Status.ReadyReplicas
					mt.Age = ageString(rs.CreationTimestamp)
				}
			}
		}

		mt.pods = append(mt.pods, podTopoItem{Name: p.Name, NodeName: node, Ready: ready, Status: status, PodIP: ip, Restarts: restarts, Age: age})
	}

	out := make([]workloadTopo, 0, len(wlMap))
	for ownerKey, wt := range wlMap {
		mids := midMap[ownerKey]
		wt.mids = make([]midTopo, 0, len(mids))
		for _, mt := range mids {
			wt.mids = append(wt.mids, *mt)
		}
		out = append(out, *wt)
	}
	return out, nil
}

func sortNamespaceTopology(rows []namespaceTopo) {
	for i := range rows {
		for wi := range rows[i].workloads {
			for mi := range rows[i].workloads[wi].mids {
				sort.Slice(rows[i].workloads[wi].mids[mi].pods, func(a, b int) bool {
					return rows[i].workloads[wi].mids[mi].pods[a].Name < rows[i].workloads[wi].mids[mi].pods[b].Name
				})
			}
			sort.Slice(rows[i].workloads[wi].mids, func(a, b int) bool {
				if rows[i].workloads[wi].mids[a].Kind != rows[i].workloads[wi].mids[b].Kind {
					return rows[i].workloads[wi].mids[a].Kind < rows[i].workloads[wi].mids[b].Kind
				}
				return rows[i].workloads[wi].mids[a].Name < rows[i].workloads[wi].mids[b].Name
			})
		}
		sort.Slice(rows[i].workloads, func(a, b int) bool {
			if rows[i].workloads[a].Kind != rows[i].workloads[b].Kind {
				return rows[i].workloads[a].Kind < rows[i].workloads[b].Kind
			}
			return rows[i].workloads[a].Name < rows[i].workloads[b].Name
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		return rows[i].Namespace < rows[j].Namespace
	})
}

func sortIngressTopology(rows []ingressTopo) {
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Namespace != rows[j].Namespace {
			return rows[i].Namespace < rows[j].Namespace
		}
		return rows[i].Name < rows[j].Name
	})
	for i := range rows {
		sort.Slice(rows[i].services, func(a, b int) bool {
			if rows[i].services[a].Name != rows[i].services[b].Name {
				return rows[i].services[a].Name < rows[i].services[b].Name
			}
			return rows[i].services[a].Port < rows[i].services[b].Port
		})
		for si := range rows[i].services {
			for wi := range rows[i].services[si].workloads {
				for mi := range rows[i].services[si].workloads[wi].mids {
					sort.Slice(rows[i].services[si].workloads[wi].mids[mi].pods, func(a, b int) bool {
						return rows[i].services[si].workloads[wi].mids[mi].pods[a].Name < rows[i].services[si].workloads[wi].mids[mi].pods[b].Name
					})
				}
				sort.Slice(rows[i].services[si].workloads[wi].mids, func(a, b int) bool {
					if rows[i].services[si].workloads[wi].mids[a].Kind != rows[i].services[si].workloads[wi].mids[b].Kind {
						return rows[i].services[si].workloads[wi].mids[a].Kind < rows[i].services[si].workloads[wi].mids[b].Kind
					}
					return rows[i].services[si].workloads[wi].mids[a].Name < rows[i].services[si].workloads[wi].mids[b].Name
				})
			}
			sort.Slice(rows[i].services[si].workloads, func(a, b int) bool {
				if rows[i].services[si].workloads[a].Kind != rows[i].services[si].workloads[b].Kind {
					return rows[i].services[si].workloads[a].Kind < rows[i].services[si].workloads[b].Kind
				}
				return rows[i].services[si].workloads[a].Name < rows[i].services[si].workloads[b].Name
			})
		}
	}
}

func printNamespaceTopology(rows []namespaceTopo) {
	for ni := range rows {
		n := rows[ni]
		fmt.Println("Namespace")
		nw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(nw, "| NAME\tWORKLOADS\tPODS")
		fmt.Fprintf(nw, "| %s\t%d\t%d\n", n.Namespace, len(n.workloads), namespacePodCount(n))
		_ = nw.Flush()

		for wi := range n.workloads {
			wl := n.workloads[wi]
			isLastWl := wi == len(n.workloads)-1
			wlBranch, wlIndent := treeBranch(nil, isLastWl)

			fmt.Printf("%s%s\n", wlBranch, strings.Title(topoKind(wl.Kind)))
			if strings.ToLower(wl.Kind) == "deployment" {
				dw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintf(dw, "%s|NAME\tREADY\tUP-TO-DATE\tAVAILABLE\tAGE\n", wlIndent)
				fmt.Fprintf(dw, "%s|%s\t%s\t%d\t%d\t%s\n", wlIndent, wl.Name, wl.Ready, wl.UpToDate, wl.Available, valueOrDash(wl.Age))
				_ = dw.Flush()
			} else {
				fmt.Printf("%s|%s/%s pods=%d\n", wlIndent, topoKind(wl.Kind), wl.Name, workloadPodCount(wl))
			}

			anc := []bool{isLastWl}
			for mi := range wl.mids {
				m := wl.mids[mi]
				isLastMid := mi == len(wl.mids)-1
				mBranch, mIndent := treeBranch(anc, isLastMid)

				if m.Kind == "" && m.Name == "" {
					fmt.Printf("%sPods\n", mBranch)
					pw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
					prefix := mIndent + "|"
					fmt.Fprintf(pw, "%sNAME\tREADY\tSTATUS\tRESTARTS\tAGE\tIP\tNODE\n", prefix)
					for i := range m.pods {
						p := m.pods[i]
						fmt.Fprintf(
							pw,
							"%s%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
							prefix,
							p.Name,
							valueOrDash(p.Ready),
							valueOrDash(p.Status),
							p.Restarts,
							valueOrDash(p.Age),
							valueOrDash(p.PodIP),
							valueOrDash(p.NodeName),
						)
					}
					_ = pw.Flush()
					continue
				}

				if strings.ToLower(m.Kind) == "replicaset" && m.Age != "" {
					fmt.Printf("%sReplicaSet\n", mBranch)
					rw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
					fmt.Fprintf(rw, "%s|NAME\tDESIRED\tCURRENT\tREADY\tAGE\n", mIndent)
					fmt.Fprintf(rw, "%s|%s\t%d\t%d\t%d\t%s\n", mIndent, m.Name, m.Desired, m.Current, m.Ready, valueOrDash(m.Age))
					_ = rw.Flush()

					podBranch, podIndent := treeBranch(append(append([]bool{}, anc...), isLastMid), true)
					fmt.Printf("%sPods\n", podBranch)
					pw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
					prefix := podIndent + "|"
					fmt.Fprintf(pw, "%sNAME\tREADY\tSTATUS\tRESTARTS\tAGE\tIP\tNODE\n", prefix)
					for i := range m.pods {
						p := m.pods[i]
						fmt.Fprintf(
							pw,
							"%s%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
							prefix,
							p.Name,
							valueOrDash(p.Ready),
							valueOrDash(p.Status),
							p.Restarts,
							valueOrDash(p.Age),
							valueOrDash(p.PodIP),
							valueOrDash(p.NodeName),
						)
					}
					_ = pw.Flush()
					continue
				}

				fmt.Printf("%s|%s/%s pods=%d\n", mIndent, topoKind(m.Kind), m.Name, len(m.pods))
			}
		}

		if ni != len(rows)-1 {
			fmt.Println()
		}
	}
}

func printIngressTopology(rows []ingressTopo) {
	for ri := range rows {
		r := rows[ri]
		fmt.Println("Ingress")
		iw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(iw, "| NAME\tCLASS\tHOSTS\tADDRESS\tPORTS\tAGE")
		fmt.Fprintf(iw, "| %s\t%s\t%s\t%s\t%s\t%s\n",
			r.Name,
			valueOrDash(r.Class),
			valueOrDash(r.Hosts),
			valueOrDash(r.Address),
			valueOrDash(r.Ports),
			valueOrDash(r.Age),
		)
		_ = iw.Flush()

		for si := range r.services {
			s := r.services[si]
			isLastSvc := si == len(r.services)-1
			svcBranch, svcIndent := treeBranch(nil, isLastSvc)

			fmt.Printf("%sService\n", svcBranch)
			sw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintf(sw, "%sNAME\tTYPE\tCLUSTER-IP\tEXTERNAL-IP\tPORT(S)\tAGE\n", svcIndent+"|")
			fmt.Fprintf(sw, "%sservice/%s\t%s\t%s\t%s\t%s\t%s\n",
				svcIndent+"|",
				s.Name,
				valueOrDash(s.Type),
				valueOrDash(s.ClusterIP),
				valueOrDash(s.ExternalIP),
				valueOrDash(s.Ports),
				valueOrDash(s.Age),
			)
			_ = sw.Flush()

			anc := []bool{isLastSvc}
			for wi := range s.workloads {
				wl := s.workloads[wi]
				isLastWl := wi == len(s.workloads)-1
				wlBranch, wlIndent := treeBranch(anc, isLastWl)

				kindTitle := strings.Title(topoKind(wl.Kind))
				fmt.Printf("%s%s\n", wlBranch, kindTitle)

				if strings.ToLower(wl.Kind) == "deployment" {
					dw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
					fmt.Fprintf(dw, "%s|NAME\tREADY\tUP-TO-DATE\tAVAILABLE\tAGE\n", wlIndent)
					fmt.Fprintf(dw, "%s|%s\t%s\t%d\t%d\t%s\n", wlIndent, wl.Name, wl.Ready, wl.UpToDate, wl.Available, valueOrDash(wl.Age))
					_ = dw.Flush()
				} else {
					fmt.Printf("%s|%s/%s pods=%d\n", wlIndent, topoKind(wl.Kind), wl.Name, workloadPodCount(wl))
				}

				anc2 := append(append([]bool{}, anc...), isLastWl)
				for mi := range wl.mids {
					m := wl.mids[mi]
					isLastMid := mi == len(wl.mids)-1
					mBranch, mIndent := treeBranch(anc2, isLastMid)

					if m.Kind == "" && m.Name == "" {
						continue
					}

					if strings.ToLower(m.Kind) == "replicaset" && m.Age != "" {
						fmt.Printf("%sReplicaSet\n", mBranch)
						rw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
						fmt.Fprintf(rw, "%s|NAME\tDESIRED\tCURRENT\tREADY\tAGE\n", mIndent)
						fmt.Fprintf(rw, "%s|%s\t%d\t%d\t%d\t%s\n", mIndent, m.Name, m.Desired, m.Current, m.Ready, valueOrDash(m.Age))
						_ = rw.Flush()
						fmt.Printf("%s└──Pods\n", mIndent)
						pw := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
						prefix := mIndent + "    |"
						fmt.Fprintf(pw, "%sNAME\tREADY\tSTATUS\tRESTARTS\tAGE\tIP\tNODE\n", prefix)
						for i := range m.pods {
							p := m.pods[i]
							fmt.Fprintf(
								pw,
								"%s%s\t%s\t%s\t%d\t%s\t%s\t%s\n",
								prefix,
								p.Name,
								valueOrDash(p.Ready),
								valueOrDash(p.Status),
								p.Restarts,
								valueOrDash(p.Age),
								valueOrDash(p.PodIP),
								valueOrDash(p.NodeName),
							)
						}
						_ = pw.Flush()
						continue
					}

					fmt.Printf("%s|%s/%s pods=%d\n", mIndent, topoKind(m.Kind), m.Name, len(m.pods))
				}
			}
		}

		if ri != len(rows)-1 {
			fmt.Println()
		}
	}
}

func podListFields(p corev1.Pod) (ready string, status string, node string, ip string, restarts int32, age string) {
	status = string(p.Status.Phase)
	node = p.Spec.NodeName
	ip = p.Status.PodIP

	total := len(p.Spec.Containers)
	readyCount := 0
	for i := range p.Status.ContainerStatuses {
		restarts += p.Status.ContainerStatuses[i].RestartCount
		if p.Status.ContainerStatuses[i].Ready {
			readyCount++
		}
	}
	if total == 0 {
		ready = "0/0"
	} else {
		ready = fmt.Sprintf("%d/%d", readyCount, total)
	}
	age = ageString(p.CreationTimestamp)
	return ready, status, node, ip, restarts, age
}

func valueOrDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func namespacePodCount(n namespaceTopo) int {
	total := 0
	for i := range n.workloads {
		total += workloadPodCount(n.workloads[i])
	}
	return total
}

func treeBranch(ancestorsLast []bool, isLast bool) (branch string, indent string) {
	var b strings.Builder
	for _, last := range ancestorsLast {
		if last {
			b.WriteString("    ")
		} else {
			b.WriteString("│   ")
		}
	}
	indent = b.String()
	if isLast {
		branch = indent + "└── "
		indent = indent + "    "
	} else {
		branch = indent + "├── "
		indent = indent + "│   "
	}
	return branch, indent
}

type ownerChain struct {
	topKind string
	topName string
	midKind string
	midName string
}

func resolveOwnerChain(ctx context.Context, clientset *kubernetes.Clientset, ns string, pod *corev1.Pod, rsCache map[string]*appsv1.ReplicaSet, jobCache map[string]*batchv1.Job) (ownerChain, bool, error) {
	ref := controllerRef(pod.OwnerReferences)
	if ref == nil {
		return ownerChain{}, false, nil
	}

	switch strings.ToLower(ref.Kind) {
	case "replicaset":
		chain := ownerChain{topKind: "ReplicaSet", topName: ref.Name, midKind: "ReplicaSet", midName: ref.Name}
		rs, err := getReplicaSet(ctx, clientset, ns, ref.Name, rsCache)
		if err != nil {
			return ownerChain{}, false, err
		}
		if rs == nil {
			chain.midKind = ""
			chain.midName = ""
			return chain, true, nil
		}
		rsRef := controllerRef(rs.OwnerReferences)
		if rsRef != nil && strings.ToLower(rsRef.Kind) == "deployment" {
			chain.topKind = "Deployment"
			chain.topName = rsRef.Name
		} else {
			chain.midKind = ""
			chain.midName = ""
		}
		return chain, true, nil
	case "job":
		chain := ownerChain{topKind: "Job", topName: ref.Name, midKind: "", midName: ""}
		j, err := getJob(ctx, clientset, ns, ref.Name, jobCache)
		if err != nil {
			return ownerChain{}, false, err
		}
		if j == nil {
			return chain, true, nil
		}
		jRef := controllerRef(j.OwnerReferences)
		if jRef != nil && strings.ToLower(jRef.Kind) == "cronjob" {
			chain.topKind = "CronJob"
			chain.topName = jRef.Name
			chain.midKind = "Job"
			chain.midName = ref.Name
		}
		return chain, true, nil
	default:
		return ownerChain{topKind: ref.Kind, topName: ref.Name, midKind: "", midName: ""}, true, nil
	}
}

func topoKind(kind string) string {
	switch strings.ToLower(kind) {
	case "deployment":
		return "deployment"
	case "statefulset":
		return "statefulset"
	case "daemonset":
		return "daemonset"
	case "job":
		return "job"
	case "cronjob":
		return "cronjob"
	case "replicaset":
		return "replicaset"
	case "replicationcontroller":
		return "replicationcontroller"
	case "pod":
		return "pod"
	default:
		return strings.ToLower(kind)
	}
}

func workloadPodCount(w workloadTopo) int {
	n := 0
	for i := range w.mids {
		n += len(w.mids[i].pods)
	}
	return n
}

func getDeployment(ctx context.Context, clientset *kubernetes.Clientset, ns, name string, cache map[string]*appsv1.Deployment) (*appsv1.Deployment, error) {
	key := ns + "/" + name
	if v, ok := cache[key]; ok {
		return v, nil
	}
	d, err := clientset.AppsV1().Deployments(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		cache[key] = nil
		return nil, nil
	}
	cache[key] = d
	return d, nil
}

func buildClientset(kubeconfigPath string) (*kubernetes.Clientset, string, error) {
	loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
	if kubeconfigPath != "" {
		loadingRules.ExplicitPath = kubeconfigPath
	}
	cfg := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, &clientcmd.ConfigOverrides{})
	restCfg, err := cfg.ClientConfig()
	if err != nil {
		return nil, "", err
	}
	ns, _, err := cfg.Namespace()
	if err != nil {
		return nil, "", err
	}
	clientset, err := kubernetes.NewForConfig(restCfg)
	if err != nil {
		return nil, "", err
	}
	return clientset, ns, nil
}

func controllerRef(refs []metav1.OwnerReference) *metav1.OwnerReference {
	for i := range refs {
		r := refs[i]
		if r.Controller != nil && *r.Controller {
			return &r
		}
	}
	return nil
}

func getReplicaSet(ctx context.Context, clientset *kubernetes.Clientset, ns, name string, cache map[string]*appsv1.ReplicaSet) (*appsv1.ReplicaSet, error) {
	key := ns + "/" + name
	if v, ok := cache[key]; ok {
		return v, nil
	}
	rs, err := clientset.AppsV1().ReplicaSets(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		cache[key] = nil
		return nil, nil
	}
	cache[key] = rs
	return rs, nil
}

func getJob(ctx context.Context, clientset *kubernetes.Clientset, ns, name string, cache map[string]*batchv1.Job) (*batchv1.Job, error) {
	key := ns + "/" + name
	if v, ok := cache[key]; ok {
		return v, nil
	}
	j, err := clientset.BatchV1().Jobs(ns).Get(ctx, name, metav1.GetOptions{})
	if err != nil {
		cache[key] = nil
		return nil, nil
	}
	cache[key] = j
	return j, nil
}
