package reserveQueues

import (
	"k8s.io/klog"
	"reflect"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

type Action struct{}

func New() *Action {
	return &Action{}
}

func (alloc *Action) Name() string {
	return "queuereserve"
}

// Initialize inits the action
func (alloc *Action) Initialize() {}

func (alloc *Action) Execute(ssn *framework.Session) {
	klog.V(3).Infof("Enter Queuereserve ...")
	defer klog.V(3).Infof("Leaving Queuereserve ...")

	for _,queue:=range ssn.Queues {
		if queue.Guarantee!=nil && len(queue.Guarantee) >0 {
			var nodes       []string
			var bindNodes   []*api.NodeInfo
			if  bindNodes = ssn.ReserveQueues(queue);bindNodes == nil {
				continue
			}
			for _ ,node:=range bindNodes {
				nodes = append(nodes,node.Name)
			}

			//if bindnodes changed ,go update the queue status
			if !reflect.DeepEqual(queue.Queue.Status.Reservation.Nodes,nodes) {

				continue
			}
			queue.Queue.Status.Reservation.Nodes = nodes

			//TODU
			//update the queue status
		}

	}
}

// UnInitialize releases resource which are not useful.
func (alloc *Action) UnInitialize() {}