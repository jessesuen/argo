package controller

import (
	"fmt"
	"testing"

	wfv1 "github.com/argoproj/argo/pkg/apis/workflow/v1alpha1"
	"github.com/stretchr/testify/assert"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TestOperateWorkflowPanicRecover ensures we can recover from unexpected panics
func TestOperateWorkflowPanicRecover(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Fail()
		}
	}()
	controller := newController()
	// intentionally set clientset to nil to induce panic
	controller.kubeclientset = nil
	wf := unmarshalWF(helloWorldWf)
	_, err := controller.wfclientset.ArgoprojV1alpha1().Workflows("").Create(wf)
	assert.Nil(t, err)
	woc := newWorkflowOperationCtx(wf, controller)
	woc.operate()
}

var sidecarWithVol = `
# Verifies sidecars can reference volumeClaimTemplates
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  name: sidecar-with-volumes
spec:
  entrypoint: sidecar-with-volumes
  volumeClaimTemplates:
  - metadata:
      name: claim-vol
    spec:
      accessModes: [ "ReadWriteOnce" ]
      resources:
        requests:
          storage: 1Gi
  volumes:
  - name: existing-vol
    persistentVolumeClaim:
      claimName: my-existing-volume
  templates:
  - name: sidecar-with-volumes
    script:
      image: python:3.6
      command: [python]
      source: |
        print("hello world")
    sidecars:
    - name: sidevol
      image: docker/whalesay:latest
      command: [sh, -c]
      args: ["echo generating message in volume; cowsay hello world | tee /mnt/vol/hello_world.txt; sleep 9999"]
      volumeMounts:
      - name: claim-vol
        mountPath: /mnt/vol
      - name: existing-vol
        mountPath: /mnt/existing-vol
`

// TestSidecarWithVolume verifies ia sidecar can have a volumeMount reference to both existing or volumeClaimTemplate volumes
func TestSidecarWithVolume(t *testing.T) {
	controller := newController()
	wfcset := controller.wfclientset.ArgoprojV1alpha1().Workflows("")
	wf := unmarshalWF(sidecarWithVol)
	wf, err := wfcset.Create(wf)
	assert.Nil(t, err)
	wf, err = wfcset.Get(wf.ObjectMeta.Name, metav1.GetOptions{})
	assert.Nil(t, err)
	woc := newWorkflowOperationCtx(wf, controller)
	woc.operate()
	assert.Equal(t, wfv1.NodeRunning, woc.wf.Status.Phase)
	pods, err := controller.kubeclientset.CoreV1().Pods(wf.ObjectMeta.Namespace).List(metav1.ListOptions{})
	assert.Nil(t, err)
	assert.True(t, len(pods.Items) > 0, "pod was not created successfully")
	pod := pods.Items[0]

	claimVolFound := false
	existingVolFound := false
	for _, ctr := range pod.Spec.Containers {
		if ctr.Name == "sidevol" {
			for _, vol := range ctr.VolumeMounts {
				if vol.Name == "claim-vol" {
					claimVolFound = true
				}
				if vol.Name == "existing-vol" {
					existingVolFound = true
				}
			}
		}
	}
	assert.True(t, claimVolFound, "claim vol was not referenced by sidecar")
	assert.True(t, existingVolFound, "existing vol was not referenced by sidecar")
}

// TestProcessNodesWithRetries tests the processNodesWithRetries() method.
func TestProcessNodesWithRetries(t *testing.T) {
	controller := newController()
	assert.NotNil(t, controller)
	wf := unmarshalWF(helloWorldWf)
	assert.NotNil(t, wf)
	woc := newWorkflowOperationCtx(wf, controller)
	assert.NotNil(t, woc)

	// Verify that there are no nodes in the wf status.
	assert.Zero(t, len(woc.wf.Status.Nodes))

	// Add the parent node for retries.
	nodeName := "test-node"
	nodeID := woc.wf.NodeID(nodeName)
	node := woc.initializeNode(nodeName, wfv1.NodeTypeRetry, "", wfv1.NodeRunning)
	retries := wfv1.RetryStrategy{}
	var retryLimit int32
	retryLimit = 2
	retries.Limit = &retryLimit
	node.RetryStrategy = &retries
	woc.wf.Status.Nodes[nodeID] = *node

	retryNodes := woc.wf.Status.GetNodesWithRetries()
	assert.Equal(t, len(retryNodes), 1)
	assert.Equal(t, node.Phase, wfv1.NodeRunning)

	// Ensure there are no child nodes yet.
	lastChild, err := woc.getLastChildNode(node)
	assert.Nil(t, err)
	assert.Nil(t, lastChild)

	// Add child nodes.
	for i := 0; i < 2; i++ {
		childNode := fmt.Sprintf("child-node-%d", i)
		woc.initializeNode(childNode, wfv1.NodeTypePod, "", wfv1.NodeRunning)
		woc.addChildNode(nodeName, childNode)
	}

	n := woc.getNodeByName(nodeName)
	lastChild, err = woc.getLastChildNode(n)
	assert.Nil(t, err)
	assert.NotNil(t, lastChild)

	// Last child is still running. processNodesWithRetries() should return false since
	// there should be no retries at this point.
	err = woc.processNodeRetries(n)
	assert.Nil(t, err)
	n = woc.getNodeByName(nodeName)
	assert.Equal(t, n.Phase, wfv1.NodeRunning)

	// Mark lastChild as successful.
	woc.markNodePhase(lastChild.Name, wfv1.NodeSucceeded)
	err = woc.processNodeRetries(n)
	assert.Nil(t, err)
	// The parent node also gets marked as Succeeded.
	n = woc.getNodeByName(nodeName)
	assert.Equal(t, n.Phase, wfv1.NodeSucceeded)

	// Mark the parent node as running again and the lastChild as failed.
	woc.markNodePhase(n.Name, wfv1.NodeRunning)
	woc.markNodePhase(lastChild.Name, wfv1.NodeFailed)
	woc.processNodeRetries(n)
	n = woc.getNodeByName(nodeName)
	assert.Equal(t, n.Phase, wfv1.NodeRunning)

	// Add a third node that has failed.
	childNode := "child-node-3"
	woc.initializeNode(childNode, wfv1.NodeTypePod, "", wfv1.NodeFailed)
	woc.addChildNode(nodeName, childNode)
	n = woc.getNodeByName(nodeName)
	err = woc.processNodeRetries(n)
	assert.Nil(t, err)
	n = woc.getNodeByName(nodeName)
	assert.Equal(t, n.Phase, wfv1.NodeFailed)
}

var workflowParallismLimit = `
apiVersion: argoproj.io/v1alpha1
kind: Workflow
metadata:
  generateName: parallelism-limit-
spec:
  entrypoint: parallelism-limit
  parallelism: 2
  templates:
  - name: parallelism-limit
    steps:
    - - name: sleep
        template: sleep
        withItems:
        - this
        - workflow
        - should
        - take
        - at
        - least
        - 60
        - seconds
        - to
        - complete

  - name: sleep
    container:
      image: alpine:latest
      command: [sh, -c, sleep 10]
`

// TestWorkflowParallismLimit verifies parallism is honored.
func TestWorkflowParallismLimit(t *testing.T) {
	controller := newController()
	wfcset := controller.wfclientset.ArgoprojV1alpha1().Workflows("")
	wf := unmarshalWF(workflowParallismLimit)
	wf, err := wfcset.Create(wf)
	assert.Nil(t, err)
	wf, err = wfcset.Get(wf.ObjectMeta.Name, metav1.GetOptions{})
	assert.Nil(t, err)
	woc := newWorkflowOperationCtx(wf, controller)
	woc.operate()
	pods, err := controller.kubeclientset.CoreV1().Pods("").List(metav1.ListOptions{})
	assert.Nil(t, err)
	assert.Equal(t, 2, len(pods.Items))
}
