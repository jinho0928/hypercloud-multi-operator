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
	"context"
	"strings"

	clusterv1alpha1 "github.com/tmax-cloud/hypercloud-multi-operator/apis/cluster/v1alpha1"
	"github.com/tmax-cloud/hypercloud-multi-operator/controllers/util"

	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/tools/clientcmd"

	"sigs.k8s.io/cluster-api/util/patch"
	ctrl "sigs.k8s.io/controller-runtime"
)

func (r *SecretReconciler) UpdateClusterManagerControlPlaneEndpoint(ctx context.Context, secret *corev1.Secret) (ctrl.Result, error) {
	key := types.NamespacedName{
		Name:      secret.Name,
		Namespace: secret.Namespace,
	}
	log := r.Log.WithValues("secret", key)
	log.Info("Start to reconcile phase for UpdateClusterManagerControleplaneEndpoint... ")

	kubeConfig, err := clientcmd.Load(secret.Data["value"])
	if err != nil {
		log.Error(err, "Failed to get kubeconfig data from secret")
		return ctrl.Result{}, err
	}

	key = types.NamespacedName{
		Name:      strings.Split(secret.Name, util.KubeconfigSuffix)[0],
		Namespace: secret.Namespace,
	}
	clm := &clusterv1alpha1.ClusterManager{}
	err = r.Get(context.TODO(), key, clm)
	if errors.IsNotFound(err) {
		log.Info("Cannot found clusterManager")
		// return ctrl.Result{RequeueAfter: requeueAfter5Sec}, nil
	} else if err != nil {
		log.Error(err, "Failed to get clusterManager + ["+clm.Name+"]")
		return ctrl.Result{}, err
	} else {
		server := kubeConfig.Clusters[kubeConfig.Contexts[kubeConfig.CurrentContext].Cluster].Server
		if !strings.EqualFold(clm.Status.ControlPlaneEndpoint, server) {
			log.Info("Update clustermanager status. add controleplane endpoint")
			helper, _ := patch.NewHelper(clm, r.Client)
			defer func() {
				if err := helper.Patch(context.TODO(), clm); err != nil {
					log.Error(err, "ClusterManager patch error")
				}
			}()
			clm.Status.ControlPlaneEndpoint = server
		}
	}

	return ctrl.Result{}, nil
}

func (r *SecretReconciler) DeployRolebinding(ctx context.Context, secret *corev1.Secret) (ctrl.Result, error) {
	log := r.Log.WithValues(
		"secret",
		types.NamespacedName{
			Name:      secret.Name,
			Namespace: secret.Namespace,
		},
	)
	log.Info("Start to reconcile phase for Deploy rolebinding to remote")

	clm := &clusterv1alpha1.ClusterManager{}
	key := types.NamespacedName{
		Name:      strings.Split(secret.Name, util.KubeconfigSuffix)[0],
		Namespace: secret.Namespace,
	}
	if err := r.Get(context.TODO(), key, clm); err != nil {
		log.Error(err, "Failed to get ClusterManager")
		return ctrl.Result{}, err
	}

	remoteClientset, err := util.GetRemoteK8sClient(secret)
	if err != nil {
		log.Error(err, "Failed to get remoteK8sClient")
		return ctrl.Result{}, err
	}

	clusterAdminCRBName := "cluster-owner-crb-" + clm.Annotations[util.AnnotationKeyOwner]
	clusterAdminCRB := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: clusterAdminCRBName,
		},
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     "cluster-admin",
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:     rbacv1.UserKind,
				APIGroup: rbacv1.GroupName,
				Name:     clm.Annotations[util.AnnotationKeyOwner],
			},
		},
	}

	targetGroup := []string{
		"",
		"apps",
		"autoscaling",
		"batch",
		"extensions",
		"policy",
		"networking.k8s.io",
		"snapshot.storage.k8s.io",
		"storage.k8s.io",
		"apiextensions.k8s.io",
		"metrics.k8s.io",
	}

	_, err = remoteClientset.
		RbacV1().
		ClusterRoleBindings().
		Get(context.TODO(), clusterAdminCRBName, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := remoteClientset.
			RbacV1().
			ClusterRoleBindings().
			Create(context.TODO(), clusterAdminCRB, metav1.CreateOptions{})
		if err != nil {
			log.Error(err, "Cannot create ClusterRoleBinding for cluster-admin")
			return ctrl.Result{}, err
		}
		log.Info("Create ClusterRoleBinding for cluster-admin to remote cluster successfully")
	} else if err != nil {
		log.Error(err, "Failed to get ClusterRoleBinding for cluster-admin from remote cluster")
		return ctrl.Result{}, err
	}

	crList := []*rbacv1.ClusterRole{
		createClusterRole("developer", targetGroup, []string{rbacv1.VerbAll}),
		createClusterRole("guest", targetGroup, []string{"get", "list", "watch"}),
	}

	for _, targetCr := range crList {
		_, err := remoteClientset.
			RbacV1().
			ClusterRoles().
			Get(context.TODO(), targetCr.Name, metav1.GetOptions{})
		if errors.IsNotFound(err) {
			_, err := remoteClientset.
				RbacV1().
				ClusterRoles().
				Create(context.TODO(), targetCr, metav1.CreateOptions{})
			if err != nil {
				log.Error(err, "Cannot create ClusteRrole ["+targetCr.Name+"] to remote cluster")
				return ctrl.Result{}, err
			}
			log.Info("Create ClusteRrole [" + targetCr.Name + "] to remote cluster successfully")
		} else if err != nil {
			log.Error(err, "Failed to get ClusteRrole ["+targetCr.Name+"] from remote cluster")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{}, nil
}

func (r *SecretReconciler) DeployArgocdResources(ctx context.Context, secret *corev1.Secret) (ctrl.Result, error) {
	log := r.Log.WithValues("secret", types.NamespacedName{Name: secret.Name, Namespace: secret.Namespace})
	log.Info("Start to reconcile phase for Deploy argocd resources to remote")

	remoteClientset, err := util.GetRemoteK8sClient(secret)
	if err != nil {
		log.Error(err, "Failed to get remoteK8sClient")
		return ctrl.Result{}, err
	}

	argocdManager := &corev1.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name: util.ArgoServiceAccount,
		},
	}
	_, err = remoteClientset.
		CoreV1().
		ServiceAccounts(util.KubeNamespace).
		Get(context.TODO(), util.ArgoServiceAccount, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := remoteClientset.
			CoreV1().
			ServiceAccounts(util.KubeNamespace).
			Create(context.TODO(), argocdManager, metav1.CreateOptions{})
		if err != nil {
			log.Error(err, "Cannot create ServiceAccount for argocd ["+util.ArgoClusterRole+"] to remote cluster")
			return ctrl.Result{}, err
		}
		log.Info("Create ServiceAccount for argocd [" + util.ArgoClusterRole + "] to remote cluster successfully")
	} else if err != nil {
		log.Error(err, "Failed to get ServiceAccount for argocd ["+util.ArgoServiceAccount+"] from remote cluster")
		return ctrl.Result{}, err
	}

	argocdManagerRole := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: util.ArgoClusterRole,
		},
		Rules: []rbacv1.PolicyRule{
			{
				APIGroups: []string{rbacv1.APIGroupAll},
				Resources: []string{rbacv1.ResourceAll},
				Verbs:     []string{rbacv1.VerbAll},
			},
			{
				NonResourceURLs: []string{rbacv1.NonResourceAll},
				Verbs:           []string{rbacv1.VerbAll},
			},
		},
	}
	_, err = remoteClientset.
		RbacV1().
		ClusterRoles().
		Get(context.TODO(), util.ArgoClusterRole, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := remoteClientset.
			RbacV1().
			ClusterRoles().
			Create(context.TODO(), argocdManagerRole, metav1.CreateOptions{})
		if err != nil {
			log.Error(err, "Cannot create ClusterRole for argocd ["+util.ArgoClusterRole+"] to remote cluster")
			return ctrl.Result{}, err
		}
		log.Info("Create ClusterRole for argocd [" + util.ArgoClusterRole + "] to remote cluster successfully")
	} else if err != nil {
		log.Error(err, "Failed to get ClusterRole for argocd ["+util.ArgoClusterRole+"] from remote cluster")
		return ctrl.Result{}, err
	}

	argocdManagerRoleBinding := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: util.ArgoClusterRoleBinding,
		},
		RoleRef: rbacv1.RoleRef{
			Kind:     "ClusterRole",
			APIGroup: rbacv1.GroupName,
			Name:     util.ArgoClusterRole,
		},
		Subjects: []rbacv1.Subject{
			{
				Kind:      rbacv1.ServiceAccountKind,
				Name:      util.ArgoServiceAccount,
				Namespace: util.ArgoNamespace,
			},
		},
	}
	_, err = remoteClientset.
		RbacV1().
		ClusterRoleBindings().
		Get(context.TODO(), util.ArgoClusterRoleBinding, metav1.GetOptions{})
	if errors.IsNotFound(err) {
		_, err := remoteClientset.
			RbacV1().
			ClusterRoleBindings().
			Create(context.TODO(), argocdManagerRoleBinding, metav1.CreateOptions{})
		if err != nil {
			log.Error(err, "Cannot create ClusterRoleBinding for argocd ["+util.ArgoClusterRoleBinding+"] to remote cluster")
			return ctrl.Result{}, err
		}
		log.Info("Create ClusterRoleBinding for argocd [" + util.ArgoClusterRoleBinding + "] to remote cluster successfully")
	} else if err != nil {
		log.Error(err, "Failed to get ClusterRoleBinding for argocd ["+util.ArgoClusterRoleBinding+"] from remote cluster")
		return ctrl.Result{}, err
	}

	return ctrl.Result{}, nil
}
