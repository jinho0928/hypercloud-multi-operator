/*
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

package controllers

import (
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
)

func getKubeConfig(secret corev1.Secret) (*rest.Config, error) {
	if value, ok := secret.Data["value"]; ok {
		if clientConfig, err := clientcmd.NewClientConfigFromBytes(value); err == nil {
			if restConfig, err := clientConfig.ClientConfig(); err == nil {
				return restConfig, nil
			}
		}
	}
	return nil, errors.NewBadRequest("getClientConfig Error")
}

func createClusterRole(name string, targetGroup []string, verbList []string) *rbacv1.ClusterRole {
	clusterRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: targetGroup,
				Resources: []string{rbacv1.ResourceAll},
				Verbs:     verbList,
			},
			{
				APIGroups: []string{"apiregistration.k8s.io"},
				Resources: []string{rbacv1.ResourceAll},
				Verbs:     []string{"get", "list", "watch"},
			},
		},
	}

	return clusterRole
}
