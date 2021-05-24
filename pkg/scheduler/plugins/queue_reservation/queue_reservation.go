/*
Copyright 2020 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package queue_reservation

import (
	"fmt"
	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
	"math"
	"strconv"
	"time"
	"volcano.sh/volcano/pkg/scheduler/api"
	"volcano.sh/volcano/pkg/scheduler/framework"
)

const
(
	PluginName = "queue_reservation"

	mb int64 = 1024 * 1024
	Gb       = mb * 1024

	NodeSumResWeight  = 0.4
	NodeUsedResWeight = 0.35
	NodeIdleResWeight = 0.15
	NodeNumWeight     = 0.1

	GpuResWeight      = 0.6
	CpuResWeightWithScalarResources = 0.3
	MemResWeightWithScalarResources = 0.1
	CpuResWeightWithOutScalarResources = 0.7
	MemResWeightWithOutScalarResources = 0.3

	GpuCardsThreshold = 2
	CpuCoresThreshold = 10*1000
	MemSizeThreshold  = 50*Gb
	WeightMinLine     = 0.2

)

func nodesScore(ssn *framework.Session,nodes []*api.NodeInfo,queueInfo *api.QueueInfo) float64 {

	if len(nodes) == 0{
		return 0
	}

	var sumResNode   [] *api.Resource
	var idleResNode  [] *api.Resource
	var usedResNode  [] *api.Resource

	for _,n:=range nodes {
		sumResNode = append(sumResNode,n.Allocatable)
		idleResNode = append(idleResNode,n.Idle)

		for _,task :=range n.Tasks {
			if task.Status != api.Running  && task.Status!= api.Allocated {
				continue
			}

			if j, found := ssn.Jobs[task.Job]; !found {
				continue
			} else if j.Queue == queueInfo.UID{
				usedResNode = append(usedResNode,task.Resreq)
			}
		}
	}

	hh :=""
	for _ ,n:=range nodes{
		hh = hh+" "+n.Name
	}

	result := float64(caculate(sumResNode,queueInfo.Guarantee))*NodeSumResWeight + float64(caculate(usedResNode,queueInfo.Guarantee))*NodeUsedResWeight +
		float64(caculate(idleResNode,queueInfo.Guarantee))*NodeIdleResWeight + float64(NodeNumWeight*100/len(nodes))

	result, _ = strconv.ParseFloat(fmt.Sprintf("%.2f", result), 64)

	return result
}

func caculate(nodes []*api.Resource,guarantee v1.ResourceList) int {
	var (
		nodeCpus []*resource.Quantity
		nodeMems []*resource.Quantity
		nodeGpus  []float64

	    cpuTarget *resource.Quantity
	    memTarget *resource.Quantity
	    gpuTarget float64

		caculateCpuWeight = CpuResWeightWithOutScalarResources
		caculateMemWeight = MemResWeightWithOutScalarResources
		isGpuNeed         = false
	)


	if _,ok:=guarantee["nvidia.com/gpu"];ok {
		caculateCpuWeight = CpuResWeightWithScalarResources
		caculateMemWeight = MemResWeightWithScalarResources
		isGpuNeed = true
	}

	for _,node :=range nodes {
		nodeCpus = append(nodeCpus,resource.NewQuantity(int64(node.MilliCPU),resource.BinarySI))
		nodeMems = append(nodeMems,resource.NewQuantity(int64(node.Memory),resource.DecimalSI))
		nodeGpus = append(nodeGpus,node.ScalarResources["nvidia.com/gpu"])
	}

	cpuTarget = guarantee.Cpu()
	memTarget = guarantee.Memory()


	score := cpuScore(caculateCpuWeight, nodeCpus, cpuTarget) + memScore(caculateMemWeight, nodeMems, memTarget)

	if isGpuNeed{
		gv :=guarantee["nvidia.com/gpu"]
		gpuTarget = float64(gv.Value())
		score = score+gpuScore(nodeGpus,gpuTarget)
	}

	return score

}

func cpuScore(weight float64 ,nodesCpus []*resource.Quantity, cpuTarget *resource.Quantity) int {
	var (
		sum    int64
		result float64
	)
	for _,res :=range nodesCpus {
		sum +=res.Value()
	}

	if math.Abs(float64(sum-cpuTarget.MilliValue())) <= CpuCoresThreshold {
		return int(100 * weight)
	}

	if result = CpuCoresThreshold / float64(sum-cpuTarget.MilliValue());result < WeightMinLine{
		result = WeightMinLine
	}

	return int(100 * weight * result)

}

func gpuScore(nodesGpus []float64, gpuTarget float64) int {
	var (
		sum    float64
		result float64
	)
	for _,res :=range nodesGpus {
		sum +=res
	}

	if math.Abs(sum/1000 - gpuTarget) <= GpuCardsThreshold {
		return 100 * GpuResWeight
	}

	if result =GpuCardsThreshold / float64(sum/1000 - gpuTarget);result < WeightMinLine {
		result = WeightMinLine
	}

	return int(100 * GpuResWeight * result)

}

func memScore(weight float64,nodesMem []*resource.Quantity, memTarget *resource.Quantity) int {
	var (
		sum    int64
		result float64
	)
	for _,res :=range nodesMem {
		sum +=res.Value()
	}

	if math.Abs(float64(sum-memTarget.Value()*Gb)) <= float64(MemSizeThreshold) {
		return int(100 * weight)
	}

	if result  =float64(MemSizeThreshold)/ float64(sum-memTarget.Value()*Gb); result < WeightMinLine {
		result = WeightMinLine
	}

	return int(100 * weight * result)

}

type queueReservationPlugin struct {
	pluginArguments framework.Arguments
}

func New(arguments framework.Arguments) framework.Plugin{
	return &queueReservationPlugin{pluginArguments: arguments}
}

func (rp *queueReservationPlugin) Name() string {
	return PluginName
}

func subsetsWithDup(queueInfo *api.QueueInfo,nums[]*api.NodeInfo) [][]*api.NodeInfo {
	var res [][]*api.NodeInfo
	var backTrack func(queueInfo *api.QueueInfo,i int, temp []*api.NodeInfo)
	var checkSub  func(a[]*api.NodeInfo ,b[]*api.NodeInfo) bool
	checkSub = func(a[]*api.NodeInfo ,b[]*api.NodeInfo)bool{
		if len(a)>len(b){
			return false
		}

		if len(a) ==0 ||len(b)==0{
			return false
		}

		for _,v:=range a{
			has:=false
			for _,k:=range b{
				if k.Name == v.Name{
					has = true
					break
				}
			}
			if !has {
				return false
			}
		}
		return true
	}

	backTrack = func(queueInfo *api.QueueInfo,i int, temp []*api.NodeInfo) {
		tmp := make([]*api.NodeInfo, len(temp))
		copy(tmp, temp)

		var (
			resource = api.EmptyResource()
			greq     = isQueueGpuReq(queueInfo)
		)

		for _,node:=range tmp{
			resource.Add(node.Allocatable)
		}

		if  resource.MilliCPU >= float64(queueInfo.Guarantee.Cpu().MilliValue()) &&
			resource.Memory   >= float64(queueInfo.Guarantee.Memory().Value()*Gb) &&
			resource.ScalarResources["nvidia.com/gpu"] >= float64(greq){

			sub:=false
			for _,check :=range res {
				if checkSub(check,tmp){
					sub = true
					break
				}
			}
			if !sub{
				res = append(res, tmp)
			}
		}

		for j := i; j < len(nums); j++ {
			if j > i && nums[j-1] == nums[j] {
				continue
			}
			tmp = append(tmp, nums[j])
			backTrack(queueInfo,j+1, tmp)
			tmp = tmp[:len(tmp)-1]
		}

	}

	backTrack(queueInfo,0, []*api.NodeInfo{})

	return res
}

func isGpuNode(node *api.NodeInfo) int64{
	if v,ok :=node.Allocatable.ScalarResources["nvidia.com/gpu"];ok && v>0{
		return int64(v)
	}

	return 0
}

func isQueueGpuReq(queueInfo *api.QueueInfo) int64{
	mutiResources :=queueInfo.Guarantee
	if v,ok:=mutiResources["nvidia.com/gpu"];ok && v.Value() > 0 {
		return v.Value()
	}
	return 0
}

func FindBestNodes(ssn *framework.Session,queueInfo *api.QueueInfo,combineList [][]*api.NodeInfo) []*api.NodeInfo {
	combineMap :=map[float64][]*api.NodeInfo{}
	for _,combine :=range combineList {
		score := nodesScore(ssn,combine,queueInfo)
		combineMap[score] = combine
	}

	var bestScore float64

	for k :=range combineMap{
		if k >bestScore{
			bestScore = k
		}
	}

	//find the best combine
	return combineMap[bestScore]

}

func (rp *queueReservationPlugin) OnSessionOpen(ssn *framework.Session) {

	// try to find the best combine nodes  to the queue for reserving
	reserveQueues :=func(queueInfo *api.QueueInfo) []*api.NodeInfo {
		t := time.Now()
		//filter first
		nodes := ssn.Nodes
		var targetNodes  []*api.NodeInfo

		//filter the GPU node from the not GPU request queue
		for _, node :=range nodes {
			isGpuRequest :=isQueueGpuReq(queueInfo)
			isGpuNode    :=isGpuNode(node)

			if (isGpuRequest>0 && isGpuNode >0) || (isGpuRequest <0 && isGpuNode <0){
				targetNodes = append(targetNodes,node)
			}
		}

		//get the nodecombines that satisfy the queues
		combineList := subsetsWithDup(queueInfo,targetNodes)
		elapsed1 := time.Since(t)
		result := FindBestNodes(ssn,queueInfo,combineList)
		elapsed2 := time.Since(t)
		fmt.Println("queue_reserve  elapsed :", elapsed1  ," elapsed2 ", elapsed2," with result ",result)

		return result

	}

	//used to find the queues's already locked nodes.
	queueReservedFn :=func(queueInfo *api.QueueInfo,queueInfos []*api.QueueInfo,nodeInfos []*api.NodeInfo) []*api.NodeInfo {

		var result []*api.NodeInfo
		nodeMap :=make(map[string]*api.NodeInfo)

		// if queue is Guarantee and already has locked nodes ,try to return locked nodes.
		if queueInfo.Queue.Spec.Guarantee.Resource != nil {
			for _,node:=range nodeInfos {
				nodeMap[node.Name] = node
			}

			reservedNodes := queueInfo.Queue.Status.Reservation.Nodes
			if len(reservedNodes) >0 {
				for _,node :=range reservedNodes {
					if _, found := nodeMap[node];found {
						result = append(result,nodeMap[node])
					}
				}
			}
		}

		if len(result)>0 {
			return result
		}

		// by default, return nodes not locked by other queues.
		for _,queueInfo := range queueInfos {
			nodes :=queueInfo.Queue.Status.Reservation.Nodes
			for _,node:=range nodes {
				nodeMap[node] = &api.NodeInfo{Name:node}
			}
		}

		for _,node:=range nodeInfos {
			if _,found:=nodeMap[node.Name] ;!found {
				result = append(result,node)
			}
		}

		return result
	}

	ssn.AddQueueReservedFn(rp.Name(),queueReservedFn)

	ssn.AddReserveQueuesFn(rp.Name(),reserveQueues)

}

func (rp *queueReservationPlugin) OnSessionClose(ssn *framework.Session) {

}