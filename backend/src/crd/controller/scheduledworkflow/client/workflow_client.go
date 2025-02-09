// Copyright 2018 The Kubeflow Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
// https://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package client

import (
	"context"
	"time"

	commonutil "github.com/kubeflow/pipelines/backend/src/common/util"
	"github.com/kubeflow/pipelines/backend/src/crd/controller/scheduledworkflow/util"
	swfapi "github.com/kubeflow/pipelines/backend/src/crd/pkg/apis/scheduledworkflow/v1beta1"
	wraperror "github.com/pkg/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	_ "k8s.io/client-go/plugin/pkg/client/auth/gcp"
	"k8s.io/client-go/tools/cache"
)

// WorkflowClient is a client to call the Workflow API.
type WorkflowClient struct {
	clientSet commonutil.ExecutionClient
	informer  commonutil.ExecutionInformer
}

// NewWorkflowClient creates an instance of the WorkflowClient.
func NewWorkflowClient(clientSet commonutil.ExecutionClient,
	informer commonutil.ExecutionInformer) *WorkflowClient {
	return &WorkflowClient{
		clientSet: clientSet,
		informer:  informer,
	}
}

// AddEventHandler adds an event handler.
func (p *WorkflowClient) AddEventHandler(funcs *cache.ResourceEventHandlerFuncs) (cache.ResourceEventHandlerRegistration, error) {
	return p.informer.AddEventHandler(funcs)
}

// HasSynced returns true if the shared informer's store has synced.
func (p *WorkflowClient) HasSynced() func() bool {
	return p.informer.HasSynced()
}

// Get returns a Workflow, given a namespace and name.
func (p *WorkflowClient) Get(namespace string, name string) (
	wf commonutil.ExecutionSpec, isNotFoundError bool, err error) {

	return p.informer.Get(namespace, name)
}

// List returns a list of workflows given the name of their ScheduledWorkflow,
// whether they are completed, and their minimum index (to avoid returning the whole list).
func (p *WorkflowClient) List(swfName string, completed bool, minIndex int64) (
	status []swfapi.WorkflowStatus, err error) {

	labelSelector := getLabelSelectorToGetWorkflows(swfName, completed, minIndex)

	workflows, err := p.informer.List(labelSelector)
	if err != nil {
		return nil, wraperror.Wrapf(err,
			"Could not retrieve workflows for scheduled workflow (%v): %v", swfName, err)
	}

	result := toWorkflowStatuses(workflows)

	return result, nil
}

func toWorkflowStatuses(workflows commonutil.ExecutionSpecList) []swfapi.WorkflowStatus {
	result := make([]swfapi.WorkflowStatus, 0)
	for _, workflow := range workflows {
		result = append(result, *toWorkflowStatus(workflow))
	}
	return result
}

func toWorkflowStatus(workflow commonutil.ExecutionSpec) *swfapi.WorkflowStatus {
	objMeta := workflow.ExecutionObjectMeta()
	execStatus := workflow.ExecutionStatus()
	return &swfapi.WorkflowStatus{
		Name:        workflow.ExecutionName(),
		Namespace:   workflow.ExecutionNamespace(),
		SelfLink:    objMeta.SelfLink,
		UID:         objMeta.UID,
		Phase:       execStatus.Condition(),
		Message:     execStatus.Message(),
		CreatedAt:   objMeta.CreationTimestamp,
		StartedAt:   execStatus.StartedAtTime(),
		FinishedAt:  execStatus.FinishedAtTime(),
		ScheduledAt: retrieveScheduledTime(workflow),
		Index:       retrieveIndex(workflow),
	}
}

func retrieveScheduledTime(workflow commonutil.ExecutionSpec) metav1.Time {
	objMeta := workflow.ExecutionObjectMeta()
	value, ok := objMeta.Labels[commonutil.LabelKeyWorkflowEpoch]
	if !ok {
		return objMeta.CreationTimestamp
	}
	result, err := commonutil.RetrieveInt64FromLabel(value)
	if err != nil {
		return objMeta.CreationTimestamp
	}
	return metav1.NewTime(time.Unix(result, 0).UTC())
}

func retrieveIndex(workflow commonutil.ExecutionSpec) int64 {
	objMeta := workflow.ExecutionObjectMeta()
	value, ok := objMeta.Labels[commonutil.LabelKeyWorkflowIndex]
	if !ok {
		return 0
	}
	result, err := commonutil.RetrieveInt64FromLabel(value)
	if err != nil {
		return 0
	}
	return result
}

// Create creates a workflow given a namespace and its specification.
func (p *WorkflowClient) Create(ctx context.Context, namespace string, workflow commonutil.ExecutionSpec) (
	commonutil.ExecutionSpec, error) {
	result, err := p.clientSet.Execution(namespace).Create(ctx, workflow, metav1.CreateOptions{})
	if err != nil {
		return nil, wraperror.Wrapf(err, "Error creating workflow in namespace (%v): %v: %+v", namespace,
			err, workflow.ToStringForStore())
	}
	return result, nil
}

func getLabelSelectorToGetWorkflows(swfName string, completed bool, minIndex int64) *labels.Selector {
	labelSelector := labels.NewSelector()
	// The Argo workflow should be active or completed
	labelSelector = labelSelector.Add(*util.GetRequirementForCompletedWorkflowOrFatal(completed))
	// The Argo workflow should be labelled with this scheduled workflow name.
	labelSelector = labelSelector.Add(*util.GetRequirementForScheduleNameOrFatal(swfName))
	// The Argo workflow should have an index greater than...
	labelSelector = labelSelector.Add(*util.GetRequirementForMinIndexOrFatal(minIndex))
	return &labelSelector
}
