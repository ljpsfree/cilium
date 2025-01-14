// Copyright 2019 Authors of Cilium
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package mock

import (
	"fmt"
	"time"

	"github.com/cilium/cilium/pkg/lock"
	"github.com/cilium/cilium/pkg/trigger"
)

type mockMetrics struct {
	mutex                 lock.RWMutex
	allocationAttempts    map[string]int64
	ipAllocations         map[string]int64
	allocatedIPs          map[string]int
	availableENIs         int
	availableIPsPerSubnet map[string]int
	nodes                 map[string]int
	ec2ApiCall            map[string]float64
	ec2RateLimit          map[string]time.Duration
	resyncCount           int64
}

// NewMockMetrics returns a new metrics implementation with a mocked backend
func NewMockMetrics() *mockMetrics {
	return &mockMetrics{
		allocationAttempts:    map[string]int64{},
		ipAllocations:         map[string]int64{},
		allocatedIPs:          map[string]int{},
		nodes:                 map[string]int{},
		availableIPsPerSubnet: map[string]int{},
		ec2ApiCall:            map[string]float64{},
		ec2RateLimit:          map[string]time.Duration{},
	}
}

func (m *mockMetrics) ENIAllocationAttempts(status, subnetID string) int64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.allocationAttempts[fmt.Sprintf("status=%s, subnetId=%s", status, subnetID)]
}

func (m *mockMetrics) IncENIAllocationAttempt(status, subnetID string) {
	m.mutex.Lock()
	m.allocationAttempts[fmt.Sprintf("status=%s, subnetId=%s", status, subnetID)]++
	m.mutex.Unlock()
}

func (m *mockMetrics) IPAllocations(subnetID string) int64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.ipAllocations["subnetId="+subnetID]
}

func (m *mockMetrics) AddIPAllocation(subnetID string, allocated int64) {
	m.mutex.Lock()
	m.ipAllocations["subnetId="+subnetID] += allocated
	m.mutex.Unlock()
}

func (m *mockMetrics) AllocatedIPs(typ string) int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.allocatedIPs[typ]
}

func (m *mockMetrics) SetAllocatedIPs(typ string, allocated int) {
	m.mutex.Lock()
	m.allocatedIPs[typ] = allocated
	m.mutex.Unlock()
}

func (m *mockMetrics) AvailableENIs() int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.availableENIs
}

func (m *mockMetrics) SetAvailableENIs(available int) {
	m.mutex.Lock()
	m.availableENIs = available
	m.mutex.Unlock()
}

func (m *mockMetrics) Nodes(category string) int {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.nodes[category]
}

func (m *mockMetrics) SetNodes(category string, nodes int) {
	m.mutex.Lock()
	m.nodes[category] = nodes
	m.mutex.Unlock()
}

func (m *mockMetrics) EC2APICall(operation, status string) float64 {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.ec2ApiCall[fmt.Sprintf("operation=%s, status=%s", operation, status)]
}

func (m *mockMetrics) ObserveEC2APICall(operation, status string, duration float64) {
	m.mutex.Lock()
	m.ec2ApiCall[fmt.Sprintf("operation=%s, status=%s", operation, status)] += duration
	m.mutex.Unlock()
}

func (m *mockMetrics) EC2RateLimit(operation string) time.Duration {
	m.mutex.RLock()
	defer m.mutex.RUnlock()
	return m.ec2RateLimit[operation]
}

func (m *mockMetrics) ObserveEC2RateLimit(operation string, delay time.Duration) {
	m.mutex.Lock()
	m.ec2RateLimit[operation] += delay
	m.mutex.Unlock()
}

func (m *mockMetrics) SetAvailableIPsPerSubnet(subnetID, availabilityZone string, available int) {
	m.mutex.Lock()
	m.availableIPsPerSubnet[fmt.Sprintf("subnetId=%s, availabilityZone=%s", subnetID, availabilityZone)] = available
	m.mutex.Unlock()
}

func (m *mockMetrics) ResyncCount() int64 {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.resyncCount
}

func (m *mockMetrics) IncResyncCount() {
	m.mutex.Lock()
	m.resyncCount++
	m.mutex.Unlock()
}

func (m *mockMetrics) DeficitResolverTrigger() trigger.MetricsObserver {
	return nil
}

func (m *mockMetrics) K8sSyncTrigger() trigger.MetricsObserver {
	return nil
}

func (m *mockMetrics) ResyncTrigger() trigger.MetricsObserver {
	return nil
}
