package kube

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

// Overview is what the Kubernetes panel shows for one namespace.
type Overview struct {
	Workloads []Workload `json:"workloads"`
	Pods      []Pod      `json:"pods"`
	Services  []Service  `json:"services"`
	Jobs      []Job      `json:"jobs"`
}

// Workload is a Deployment, StatefulSet or DaemonSet.
type Workload struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Ready   int    `json:"ready"`
	Desired int    `json:"desired"`
	Updated int    `json:"updated"`
	Created string `json:"created"`
}

// Pod is a pod with the status kubectl get pods shows.
type Pod struct {
	Name       string   `json:"name"`
	Status     string   `json:"status"` // Running, Pending, CrashLoopBackOff, Completed, Terminating…
	Healthy    bool     `json:"healthy"`
	Ready      int      `json:"ready"`
	Total      int      `json:"total"`
	Restarts   int      `json:"restarts"`
	Node       string   `json:"node,omitempty"`
	Owner      string   `json:"owner,omitempty"` // workload name (Deployment for ReplicaSet pods)
	Containers []string `json:"containers"`
	Ports      []int    `json:"ports"`
	Created    string   `json:"created"`
}

// Service is a Service with its ports.
type Service struct {
	Name      string        `json:"name"`
	Type      string        `json:"type"`
	ClusterIP string        `json:"clusterIP"`
	Ports     []ServicePort `json:"ports"`
}

// ServicePort is one port of a Service.
type ServicePort struct {
	Port     int    `json:"port"`
	Target   string `json:"target"`
	Protocol string `json:"protocol"`
	NodePort int    `json:"nodePort,omitempty"`
}

// Job is a Job or a CronJob.
type Job struct {
	Kind     string `json:"kind"`
	Name     string `json:"name"`
	Status   string `json:"status"`
	Schedule string `json:"schedule,omitempty"`
	Created  string `json:"created"`
}

// object is the union of the fields read from every kind.
type object struct {
	Kind     string `json:"kind"`
	Metadata struct {
		Name              string            `json:"name"`
		CreationTimestamp string            `json:"creationTimestamp"`
		DeletionTimestamp string            `json:"deletionTimestamp"`
		Labels            map[string]string `json:"labels"`
		OwnerReferences   []struct {
			Kind string `json:"kind"`
			Name string `json:"name"`
		} `json:"ownerReferences"`
	} `json:"metadata"`
	Spec struct {
		Replicas    *int   `json:"replicas"`
		NodeName    string `json:"nodeName"`
		Type        string `json:"type"`
		ClusterIP   string `json:"clusterIP"`
		Schedule    string `json:"schedule"`
		Suspend     *bool  `json:"suspend"`
		Completions *int   `json:"completions"`
		Containers  []struct {
			Name  string `json:"name"`
			Ports []struct {
				ContainerPort int `json:"containerPort"`
			} `json:"ports"`
		} `json:"containers"`
		Ports []struct {
			Port       int             `json:"port"`
			TargetPort json.RawMessage `json:"targetPort"`
			Protocol   string          `json:"protocol"`
			NodePort   int             `json:"nodePort"`
		} `json:"ports"`
	} `json:"spec"`
	Status struct {
		Phase                  string `json:"phase"`
		ReadyReplicas          int    `json:"readyReplicas"`
		UpdatedReplicas        int    `json:"updatedReplicas"`
		DesiredNumberScheduled int    `json:"desiredNumberScheduled"`
		NumberReady            int    `json:"numberReady"`
		UpdatedNumberScheduled int    `json:"updatedNumberScheduled"`
		Active                 int    `json:"active"`
		Succeeded              int    `json:"succeeded"`
		Failed                 int    `json:"failed"`
		LastScheduleTime       string `json:"lastScheduleTime"`
		ContainerStatuses      []struct {
			Ready        bool `json:"ready"`
			RestartCount int  `json:"restartCount"`
			State        struct {
				Waiting *struct {
					Reason string `json:"reason"`
				} `json:"waiting"`
				Terminated *struct {
					Reason string `json:"reason"`
				} `json:"terminated"`
			} `json:"state"`
		} `json:"containerStatuses"`
	} `json:"status"`
}

// overviewKinds are fetched in one kubectl call.
const overviewKinds = "deployments,statefulsets,daemonsets,pods,services,jobs,cronjobs"

// Overview lists a namespace's workloads, pods, services and jobs.
func (k *Kubectl) Overview(ctx context.Context, kubeContext, namespace string) (Overview, error) {
	args, err := scope(kubeContext, namespace)
	if err != nil {
		return Overview{}, err
	}
	out, err := k.run(ctx, readTimeout, append(args, "get", overviewKinds, "-o", "json")...)
	if err != nil {
		return Overview{}, err
	}
	return parseOverview(out)
}

func parseOverview(data []byte) (Overview, error) {
	var list struct {
		Items []object `json:"items"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return Overview{}, fmt.Errorf("unexpected kubectl output: %w", err)
	}
	ov := Overview{Workloads: []Workload{}, Pods: []Pod{}, Services: []Service{}, Jobs: []Job{}}
	for _, o := range list.Items {
		created := o.Metadata.CreationTimestamp
		switch o.Kind {
		case "Deployment", "StatefulSet":
			desired := 1
			if o.Spec.Replicas != nil {
				desired = *o.Spec.Replicas
			}
			ov.Workloads = append(ov.Workloads, Workload{Kind: o.Kind, Name: o.Metadata.Name, Ready: o.Status.ReadyReplicas, Desired: desired, Updated: o.Status.UpdatedReplicas, Created: created})
		case "DaemonSet":
			ov.Workloads = append(ov.Workloads, Workload{Kind: o.Kind, Name: o.Metadata.Name, Ready: o.Status.NumberReady, Desired: o.Status.DesiredNumberScheduled, Updated: o.Status.UpdatedNumberScheduled, Created: created})
		case "Pod":
			ov.Pods = append(ov.Pods, pod(o))
		case "Service":
			ov.Services = append(ov.Services, service(o))
		case "Job":
			if len(o.Metadata.OwnerReferences) > 0 && o.Metadata.OwnerReferences[0].Kind == "CronJob" {
				continue // shown under its CronJob's schedule; runs are many
			}
			ov.Jobs = append(ov.Jobs, Job{Kind: "Job", Name: o.Metadata.Name, Status: jobStatus(o), Created: created})
		case "CronJob":
			status := "scheduled"
			if o.Spec.Suspend != nil && *o.Spec.Suspend {
				status = "suspended"
			}
			ov.Jobs = append(ov.Jobs, Job{Kind: "CronJob", Name: o.Metadata.Name, Status: status, Schedule: o.Spec.Schedule, Created: created})
		}
	}
	slices.SortFunc(ov.Workloads, func(a, b Workload) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(ov.Pods, func(a, b Pod) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(ov.Services, func(a, b Service) int { return strings.Compare(a.Name, b.Name) })
	slices.SortFunc(ov.Jobs, func(a, b Job) int { return strings.Compare(a.Name, b.Name) })
	return ov, nil
}

// pod derives the status the way kubectl get pods prints it.
func pod(o object) Pod {
	p := Pod{Name: o.Metadata.Name, Status: o.Status.Phase, Node: o.Spec.NodeName, Created: o.Metadata.CreationTimestamp, Containers: []string{}, Ports: []int{}}
	for _, c := range o.Spec.Containers {
		p.Containers = append(p.Containers, c.Name)
		for _, port := range c.Ports {
			p.Ports = append(p.Ports, port.ContainerPort)
		}
	}
	p.Total = len(o.Spec.Containers)
	for _, cs := range o.Status.ContainerStatuses {
		p.Restarts += cs.RestartCount
		if cs.Ready {
			p.Ready++
		}
		switch {
		case cs.State.Waiting != nil && cs.State.Waiting.Reason != "":
			p.Status = cs.State.Waiting.Reason
		case cs.State.Terminated != nil && cs.State.Terminated.Reason != "" && o.Status.Phase != "Running":
			p.Status = cs.State.Terminated.Reason
		}
	}
	if o.Metadata.DeletionTimestamp != "" {
		p.Status = "Terminating"
	}
	if p.Status == "" {
		p.Status = "Unknown"
	}
	p.Healthy = (p.Status == "Running" && p.Ready == p.Total) || p.Status == "Completed" || o.Status.Phase == "Succeeded"
	if len(o.Metadata.OwnerReferences) > 0 {
		owner := o.Metadata.OwnerReferences[0]
		p.Owner = owner.Name
		if hash := o.Metadata.Labels["pod-template-hash"]; owner.Kind == "ReplicaSet" && hash != "" {
			p.Owner = strings.TrimSuffix(owner.Name, "-"+hash) // the Deployment
		}
	}
	return p
}

func service(o object) Service {
	s := Service{Name: o.Metadata.Name, Type: o.Spec.Type, ClusterIP: o.Spec.ClusterIP, Ports: []ServicePort{}}
	for _, p := range o.Spec.Ports {
		target := strings.Trim(string(p.TargetPort), `"`)
		s.Ports = append(s.Ports, ServicePort{Port: p.Port, Target: target, Protocol: p.Protocol, NodePort: p.NodePort})
	}
	return s
}

func jobStatus(o object) string {
	completions := 1
	if o.Spec.Completions != nil {
		completions = *o.Spec.Completions
	}
	switch {
	case o.Status.Active > 0:
		return "running"
	case o.Status.Succeeded >= completions:
		return "complete"
	case o.Status.Failed > 0:
		return "failed (" + strconv.Itoa(o.Status.Failed) + ")"
	}
	return "pending"
}
