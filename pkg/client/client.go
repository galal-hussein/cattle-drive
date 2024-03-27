package client

import (
	"context"

	"github.com/rancher/lasso/pkg/client"
	"github.com/rancher/lasso/pkg/controller"
	v3 "github.com/rancher/rancher/pkg/apis/cluster.cattle.io/v3"
	"github.com/rancher/wrangler/pkg/clients"
	v1 "github.com/rancher/wrangler/pkg/generated/controllers/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/rest"
)

type Clients struct {
	Clusters                    *client.Client
	Projects                    *client.Client
	ProjectRoleTemplateBindings *client.Client
	ClusterRoleTemplateBindings *client.Client
	ClusterRegistrationTokens   *client.Client
	Users                       *client.Client
	ClusterRepos                *client.Client
	GlobalRoleBindings          *client.Client
	ConfigMaps                  v1.ConfigMapClient
	Namespace                   v1.NamespaceClient
}

func New(ctx context.Context, rest *rest.Config) (*Clients, error) {
	clients, err := clients.NewFromConfig(rest, nil)
	if err != nil {
		return nil, err
	}

	if err := clients.Start(ctx); err != nil {
		return nil, err
	}

	localSchemeBuilder := runtime.SchemeBuilder{
		v3.AddToScheme,
	}
	scheme := runtime.NewScheme()
	err = localSchemeBuilder.AddToScheme(scheme)
	if err != nil {
		return nil, err
	}
	factory, err := controller.NewSharedControllerFactoryFromConfig(rest, scheme)
	if err != nil {
		return nil, err
	}

	return &Clients{
		ConfigMaps:                  clients.Core.ConfigMap(),
		Namespace:                   clients.Core.Namespace(),
		Users:                       NewClient(factory, "management.cattle.io", "v3", "users", "User", false),
		Clusters:                    NewClient(factory, "management.cattle.io", "v3", "clusters", "Cluster", false),
		Projects:                    NewClient(factory, "management.cattle.io", "v3", "projects", "Project", true),
		ProjectRoleTemplateBindings: NewClient(factory, "management.cattle.io", "v3", "projectRoleTemplateBindings", "ProjectRoleTemplateBinding", true),
		ClusterRoleTemplateBindings: NewClient(factory, "management.cattle.io", "v3", "clusterRoleTemplateBindings", "ClusterRoleTemplateBinding", true),
		ClusterRegistrationTokens:   NewClient(factory, "management.cattle.io", "v3", "clusterRegistrationTokens", "ClusterRegistrationToken", false),
		ClusterRepos:                NewClient(factory, "catalog.cattle.io", "v1", "clusterRepos", "ClusterRepo", false),
		GlobalRoleBindings:          NewClient(factory, "management.cattle.io", "v3", "globalRoleBindings", "GlobalRoleBinding", false),
	}, nil
}

func NewClient(factory controller.SharedControllerFactory, group, version, resource, kind string, namespaced bool) *client.Client {
	gvr := schema.GroupVersionResource{Group: group, Resource: resource, Version: version}
	sharedController := factory.ForResourceKind(gvr, kind, namespaced)
	return sharedController.Client()
}
