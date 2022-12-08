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

// nolint:golint // stop check lint issues as this file will be refactored
package huaweicloud

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	huaweicloudsdkbasic "github.com/huaweicloud/huaweicloud-sdk-go-v3/core/auth/basic"
	huaweicloudsdkconfig "github.com/huaweicloud/huaweicloud-sdk-go-v3/core/config"
	huaweicloudsdkerr "github.com/huaweicloud/huaweicloud-sdk-go-v3/core/sdkerr"
	huaweicloudsdkecs "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2"
	huaweicloudsdkecsmodel "github.com/huaweicloud/huaweicloud-sdk-go-v3/services/ecs/v2/model"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/types"
	cloudprovider "k8s.io/cloud-provider"
	"k8s.io/klog"
)

// instance status
const (
	instanceShutoff = "SHUTOFF"
)

// prefix added to the server ID to form the prefix ID
const (
	providerPrefix = ProviderName + "://"
)

// Only define the ECS error code what we care about.
// More error code please refer to: https://support.huaweicloud.com/en-us/api-ecs/ecs_07_0002.html
const (
	// ECSErrorNotExist means the ECS cannot be detected.
	ECSErrorNotExist = "Ecs.0114"
)

// ErrNotFound is used to inform that the object is missing
var ErrNotFound = errors.New("failed to find object")

// ErrMultipleResults is used when we unexpectedly get back multiple results
var ErrMultipleResults = errors.New("multiple results where only one expected")

// Instances encapsulates an implementation of Instances.
type Instances struct {
	GetECSClientFunc func() *huaweicloudsdkecs.EcsClient
}

// Check if our Instances implements necessary interface
var _ cloudprovider.Instances = &Instances{}

// We can't use cloudservers.Address directly as the type of `Version` is different.
type Address struct {
	Addr string `json:"addr"`
	Type string `mapstructure:"OS-EXT-IPS:type"`
}

// NodeAddresses returns the addresses of the specified instance.
// TODO(roberthbailey): This currently is only used in such a way that it
// returns the address of the calling instance. We should do a rename to
// make this clearer.
func (i *Instances) NodeAddresses(ctx context.Context, name types.NodeName) ([]v1.NodeAddress, error) {
	klog.Infof("NodeAddresses is called. input name: %s", name)
	return nil, cloudprovider.NotImplemented
}

// NodeAddressesByProviderID returns the addresses of the specified instance.
// The instance is specified using the providerID of the node. The
// ProviderID is a unique identifier of the node. This will not be called
// from the node whose nodeaddresses are being queried. i.e. local metadata
// services cannot be used in this method to obtain nodeaddresses
func (i *Instances) NodeAddressesByProviderID(ctx context.Context, providerID string) ([]v1.NodeAddress, error) {
	klog.Infof("NodeAddressesByProviderID is called. input provider ID: %s", providerID)

	ecs, err := i.getECSByProviderID(providerID)
	if err != nil {
		klog.Errorf("Get server info failed. provider id: %s, error: %v", providerID, err)
		return nil, err
	}
	klog.V(4).Infof("server info: %s", ecs.String())

	nodeAddresses, err := i.parseAddressesFromServer(ecs)
	if err != nil {
		klog.Errorf("parse node address from server info failed. provider id: %s, error: %v", providerID, err)
		return nil, err
	}

	klog.Infof("NodeAddressesByProviderID, input provider ID: %s, output addresses: %v", providerID, nodeAddresses)

	return nodeAddresses, nil
}

// InstanceID returns the cloud provider ID of the node with the specified NodeName.
// Note that if the instance does not exist, we must return ("", cloudprovider.InstanceNotFound)
// cloudprovider.InstanceNotFound should NOT be returned for instances that exist but are stopped/sleeping
func (i *Instances) InstanceID(ctx context.Context, nodeName types.NodeName) (string, error) {
	klog.Infof("InstanceID is called. input nodeName: %s", string(nodeName))

	server, err := i.getECSByName(string(nodeName))
	if err != nil {
		klog.Warningf("failed to get ECS by name: %s, error: %s", string(nodeName), err)
		return "", err
	}

	return server.Id, nil
}

// InstanceType returns the type of the specified instance.
func (i *Instances) InstanceType(ctx context.Context, name types.NodeName) (string, error) {
	klog.Infof("InstanceType is called. input name: %s", name)
	return "", cloudprovider.NotImplemented
}

// InstanceTypeByProviderID returns the type of the specified instance.
func (i *Instances) InstanceTypeByProviderID(ctx context.Context, providerID string) (string, error) {
	klog.Infof("InstanceTypeByProviderID is called. input provider ID: %s", providerID)

	server, err := i.getECSByProviderID(providerID)
	if err != nil {
		klog.Errorf("Get server info failed. provider id: %s, error: %v", providerID, err)
		return "", err
	}

	return i.parseInstanceTypeFromServerInfo(server)
}

// AddSSHKeyToAllInstances adds an SSH public key as a legal identity for all instances
// expected format for the key is standard ssh-keygen format: <protocol> <blob>
func (i *Instances) AddSSHKeyToAllInstances(ctx context.Context, user string, keyData []byte) error {
	klog.Infof("AddSSHKeyToAllInstances is called. input user: %s, keyData: %v", user, keyData)
	return cloudprovider.NotImplemented
}

// CurrentNodeName returns the name of the node we are currently running on
// On most clouds (e.g. GCE) this is the hostname, so we provide the hostname
func (i *Instances) CurrentNodeName(ctx context.Context, hostname string) (types.NodeName, error) {
	klog.Infof("CurrentNodeName is called. input hostname: %s, output node name: %s", hostname, hostname)
	return types.NodeName(hostname), nil
}

// InstanceExistsByProviderID returns true if the instance for the given provider exists.
// If false is returned with no error, the instance will be immediately deleted by the cloud controller manager.
// This method should still return true for instances that exist but are stopped/sleeping.
func (i *Instances) InstanceExistsByProviderID(ctx context.Context, providerID string) (bool, error) {
	klog.Infof("InstanceExistsByProviderID is called. input provider ID: %s", providerID)

	_, err := i.getECSByProviderID(providerID)
	if err != nil {
		if i.isNonExistError(err) {
			klog.Infof("Instance not exist. provider ID: %s", providerID)
			return false, nil
		}

		klog.Errorf("Get server info failed. provider id: %s, error: %v", providerID, err)
		return false, err
	}

	return true, nil
}

// InstanceShutdownByProviderID returns true if the instance is shutdown in cloudprovider
func (i *Instances) InstanceShutdownByProviderID(ctx context.Context, providerID string) (bool, error) {
	klog.Infof("InstanceShutdownByProviderID is called. input provider ID: %s", providerID)

	server, err := i.getECSByProviderID(providerID)
	if err != nil {
		klog.Errorf("Get server info failed. provider id: %s, error: %v", providerID, err)
		return false, err
	}
	if server.Status == instanceShutoff {
		klog.Warningf("instance has been shut down. provider id: %s", providerID)
		return true, err
	}

	return false, err
}

func (i *Instances) parseAddressesFromServer(server *huaweicloudsdkecsmodel.ServerDetail) ([]v1.NodeAddress, error) {
	var nodeAddresses []v1.NodeAddress

	for _, addrs := range server.Addresses {
		var addressType v1.NodeAddressType
		for i := range addrs {
			if addrs[i].OSEXTIPStype == huaweicloudsdkecsmodel.GetServerAddressOSEXTIPStypeEnum().FIXED {
				addressType = v1.NodeInternalIP
			} else if addrs[i].OSEXTIPStype == huaweicloudsdkecsmodel.GetServerAddressOSEXTIPStypeEnum().FLOATING {
				addressType = v1.NodeExternalIP
			} else {
				continue
			}
			klog.V(4).Infof("get a node address, type: %s, address: %s", addressType, addrs[i].Addr)
			nodeAddresses = append(nodeAddresses, v1.NodeAddress{Type: addressType, Address: addrs[i].Addr})
		}
	}

	return nodeAddresses, nil
}

// isNonExistError tells if an error means server not exit, particularly in case of retrieve an server.
// The error looks like as follows:
//
//	{
//	   "status_code": 404,
//	   "request_id": "bb0361f3e5893add5b6810eef1123b41",
//	   "error_code": "Ecs.0114",
//	   "error_message": "Instance[a44af098-7548-4519-8243-a88ba3e5de4fnoexist] could not be found."
//	}
func (i *Instances) isNonExistError(originErr error) bool {
	sre := huaweicloudsdkerr.ServiceResponseError{}

	// parse error message to 'ServiceResponseError'
	if err := json.Unmarshal([]byte(originErr.Error()), &sre); err != nil {
		klog.Warningf("unmarshal error: %v", err)
		return false
	}

	if sre.ErrorCode == ECSErrorNotExist {
		return true
	}

	return false
}

func (i *Instances) parseInstanceTypeFromServerInfo(server *huaweicloudsdkecsmodel.ServerDetail) (string, error) {
	if server.Flavor == nil {
		return "", fmt.Errorf("failed to parse instance type from server as flavor info missing, server ID: %s", server.Id)
	}

	if len(server.Flavor.Id) == 0 {
		return "", fmt.Errorf("flavor ID is empty, server ID: %s", server.Id)
	}

	return server.Flavor.Id, nil
}

func (i *Instances) getECSByProviderID(providerID string) (*huaweicloudsdkecsmodel.ServerDetail, error) {
	client := i.GetECSClientFunc()
	if client == nil {
		return nil, fmt.Errorf("create ECS client failed with provider id: %s", providerID)
	}

	// Strip the provider name prefix to get the server ID, note that
	// providerID without prefix is still accepted for backward compatibility.
	serverID := strings.TrimPrefix(providerID, providerPrefix)

	options := &huaweicloudsdkecsmodel.ShowServerRequest{
		ServerId: serverID,
	}

	rsp, err := client.ShowServer(options)
	if err != nil || rsp == nil {
		klog.Warningf("failed to retrieve server by server ID: %s, error: %v", serverID, err)
		return nil, err
	}

	return rsp.Server, nil
}

func (i *Instances) getECSByName(name string) (*huaweicloudsdkecsmodel.ServerDetail, error) {
	client := i.GetECSClientFunc()
	if client == nil {
		return nil, fmt.Errorf("create ECS client failed with name: %s", name)
	}

	options := &huaweicloudsdkecsmodel.ListServersDetailsRequest{
		Name: &name,
	}
	rsp, err := client.ListServersDetails(options)
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve server list by name: %s, error: %w", name, err)
	}

	// If no server found, the count will be 0.
	if rsp.Count == nil || *rsp.Count == 0 {
		klog.Warningf("no server found with name: %s", name)

		return nil, cloudprovider.InstanceNotFound
	}

	if *rsp.Count > 1 {
		return nil, fmt.Errorf("found more than one server with same name: %s, which is not allowed", name)
	}

	return &rsp.Servers[0], nil
}

// getECSClient initializes a ECS(Elastic Cloud Server) client which will be used to operate ECS.
func (a *AuthOpts) getECSClient() *huaweicloudsdkecs.EcsClient {
	// There are two types of services provided by HUAWEI CLOUD according to scope:
	// - Regional services: most of services belong to this classification, such as ECS.
	// - Global services: such as IAM, TMS, EPS.
	// For Regional services' authentication, projectId is required.
	// For global services' authentication, domainId is required.
	// More details please refer to:
	// https://github.com/huaweicloud/huaweicloud-sdk-go-v3/blob/0281b9734f0f95ed5565729e54d96e9820262426/README.md#use-go-sdk
	credentials := huaweicloudsdkbasic.NewCredentialsBuilder().
		WithAk(a.AccessKey).
		WithSk(a.SecretKey).
		WithProjectId(a.ProjectID).
		Build()

	client := huaweicloudsdkecs.EcsClientBuilder().
		WithEndpoint(a.ECSEndpoint).
		WithCredential(credentials).
		WithHttpConfig(huaweicloudsdkconfig.DefaultHttpConfig()).
		Build()

	return huaweicloudsdkecs.NewEcsClient(client)
}
