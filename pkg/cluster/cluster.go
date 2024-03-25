package cluster

import (
	"context"
	"errors"
	"fmt"
	"galal-hussein/cattle-drive/pkg/client"
	"io"
	"reflect"

	v1catalog "github.com/rancher/rancher/pkg/apis/catalog.cattle.io/v1"
	v3 "github.com/rancher/rancher/pkg/apis/management.cattle.io/v3"
	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type Cluster struct {
	Obj        *v3.Cluster
	ToMigrate  ToMigrate
	SystemUser *v3.User
	Client     *client.Clients
}

type ToMigrate struct {
	Projects []*Project
	CRTBs    []*ClusterRoleTemplateBinding
	// apps related objects
	ClusterRepos []*ClusterRepo
	Apps         []*App
}

// Populate will fill in the objects to be migrated
func (c *Cluster) Populate(ctx context.Context, client *client.Clients) error {
	var (
		projects                    v3.ProjectList
		projectRoleTemplateBindings v3.ProjectRoleTemplateBindingList
		clusterRoleTemplateBindings v3.ClusterRoleTemplateBindingList
		users                       v3.UserList
		repos                       v1catalog.ClusterRepoList
	)
	// systemUsers
	if err := client.Users.List(ctx, "", &users, v1.ListOptions{}); err != nil {
		return err
	}
	for _, user := range users.Items {
		for _, principalID := range user.PrincipalIDs {
			if principalID == "system://"+c.Obj.Name {
				c.SystemUser = user.DeepCopy()
				break
			}
		}
	}
	// namespaces
	namespaces, err := c.Client.Namespace.List(v1.ListOptions{})
	if err != nil {
		return err
	}
	// projects
	if err := client.Projects.List(ctx, c.Obj.Name, &projects, v1.ListOptions{}); err != nil {
		return err
	}
	pList := []*Project{}
	for _, p := range projects.Items {
		// skip default projects before listing their prtb or roles
		if p.Spec.DisplayName == "Default" || p.Spec.DisplayName == "System" {
			continue
		}
		// prtbs
		if err := client.ProjectRoleTemplateBindings.List(ctx, p.Name, &projectRoleTemplateBindings, v1.ListOptions{}); err != nil {
			return err
		}
		prtbList := []*ProjectRoleTemplateBinding{}
		for _, item := range projectRoleTemplateBindings.Items {
			if item.Name == "creator-project-owner" {
				continue
			}
			prtb := newPRTB(item, "", p.Spec.DisplayName)
			prtb.normalize()
			if err := prtb.SetDescription(ctx, client); err != nil {
				return err
			}
			prtbList = append(prtbList, prtb)
		}
		nsList := []*Namespace{}
		for _, ns := range namespaces.Items {
			if projectID, ok := ns.Labels[projectIDLabelAnnotation]; ok && projectID == p.Name {
				n := newNamespace(ns, "", p.Spec.DisplayName)
				n.normalize()
				nsList = append(nsList, n)
			}
		}
		p := newProject(p, prtbList, nsList)
		p.normalize()
		pList = append(pList, p)
	}

	crtbList := []*ClusterRoleTemplateBinding{}
	if err := client.ClusterRoleTemplateBindings.List(ctx, c.Obj.Name, &clusterRoleTemplateBindings, v1.ListOptions{}); err != nil {
		return err
	}
	for _, item := range clusterRoleTemplateBindings.Items {
		crtb, isDefault := newCRTB(item, c.SystemUser)
		if isDefault {
			continue
		}
		crtb.normalize()
		if err := crtb.SetDescription(ctx, client); err != nil {
			return err
		}
		crtbList = append(crtbList, crtb)
	}
	// apps
	// cluster repos
	reposList := []*ClusterRepo{}
	if err := c.Client.ClusterRepos.List(ctx, "", &repos, v1.ListOptions{}); err != nil {
		return err
	}
	for _, item := range repos.Items {
		repo, isDefault := newClusterRepo(item)
		if isDefault {
			continue
		}
		repo.normalize()
		reposList = append(reposList, repo)

	}

	c.ToMigrate = ToMigrate{
		Projects:     pList,
		CRTBs:        crtbList,
		ClusterRepos: reposList,
	}
	return nil
}

// Compare will compare between objects of downstream source cluster and target cluster
func (c *Cluster) Compare(ctx context.Context, tc *Cluster) error {
	// projects
	for _, sProject := range c.ToMigrate.Projects {
		for _, tProject := range tc.ToMigrate.Projects {
			if sProject.Name == tProject.Name {
				sProject.Migrated = true
				if !reflect.DeepEqual(sProject.Obj.Spec, tProject.Obj.Spec) {
					sProject.Diff = true
					break
				} else {
					// its critical to adjust the project name here because its used in different other objects ns/prtbs
					sProject.Obj.Name = tProject.Obj.Name
					for _, sPRTB := range sProject.PRTBs {
						sPRTB.ProjectName = tProject.Obj.Name
					}
					for _, ns := range sProject.Namespaces {
						ns.ProjectName = tProject.Obj.Name
					}
				}
				// now we check for prtbs related to that project
				for _, sPrtb := range sProject.PRTBs {
					for _, tPrtb := range tProject.PRTBs {
						if sPrtb.Name == tPrtb.Name {
							sPrtb.Migrated = true
							if !reflect.DeepEqual(sPrtb.Obj, tPrtb.Obj) {
								sPrtb.Diff = true
							}
						}
					}
				}
				// namespaces
				for _, ns := range sProject.Namespaces {
					for _, tns := range tProject.Namespaces {
						if ns.Name == tns.Name {
							ns.Migrated = true
							if !reflect.DeepEqual(ns.Obj, tns.Obj) {
								ns.Diff = true
							}
						}
					}
				}
			}
		}
	}

	// crtbs
	for _, sCrtb := range c.ToMigrate.CRTBs {
		for _, tCrtb := range tc.ToMigrate.CRTBs {
			if sCrtb.Name == tCrtb.Name {
				sCrtb.Migrated = true
				if !reflect.DeepEqual(sCrtb.Obj, tCrtb.Obj) {
					sCrtb.Diff = true
					break
				}
			}
		}
	}

	for _, sRepo := range c.ToMigrate.ClusterRepos {
		for _, tRepo := range tc.ToMigrate.ClusterRepos {
			if sRepo.Name == tRepo.Name {
				sRepo.Migrated = true
				if !reflect.DeepEqual(sRepo.Obj.Spec, tRepo.Obj.Spec) {
					sRepo.Diff = true
					break
				}
			}
		}
	}
	return nil
}

func (c *Cluster) Status(ctx context.Context) error {
	fmt.Printf("Project status:\n")
	for _, p := range c.ToMigrate.Projects {
		print(p.Name, p.Migrated, p.Diff, 0)
		if len(p.PRTBs) > 0 {
			fmt.Printf("  -> users permissions:\n")
		}
		for _, prtb := range p.PRTBs {
			print(prtb.Name+": "+prtb.Description, prtb.Migrated, prtb.Diff, 1)
		}
		if len(p.Namespaces) > 0 {
			fmt.Printf("  -> namespaces:\n")
		}
		for _, ns := range p.Namespaces {
			print(ns.Name, ns.Migrated, ns.Diff, 1)
		}

	}
	fmt.Printf("Cluster users permissions:\n")
	for _, crtb := range c.ToMigrate.CRTBs {
		print(crtb.Name+": "+crtb.Description, crtb.Migrated, crtb.Diff, 0)
	}
	fmt.Printf("Catalog repos:\n")
	for _, repo := range c.ToMigrate.ClusterRepos {
		print(repo.Name, repo.Migrated, repo.Diff, 0)
	}
	return nil
}

func (c *Cluster) Migrate(ctx context.Context, client *client.Clients, tc *Cluster, w io.Writer) error {
	fmt.Fprintf(w, "Migrating Objects from cluster [%s] to cluster [%s]:\n", c.Obj.Spec.DisplayName, tc.Obj.Spec.DisplayName)
	for _, p := range c.ToMigrate.Projects {
		if !p.Migrated {
			fmt.Fprintf(w, "- migrating Project [%s]... ", p.Name)
			p.Mutate(tc)
			if err := client.Projects.Create(ctx, tc.Obj.Name, p.Obj, nil, v1.CreateOptions{}); err != nil {
				return err
			}
			// set ProjectName for all ns and prtbs for this project
			for _, sPRTB := range p.PRTBs {
				sPRTB.ProjectName = p.Obj.Name
			}
			for _, ns := range p.Namespaces {
				ns.ProjectName = p.Obj.Name
			}
			fmt.Fprintf(w, "Done.\n")
		}

		for _, prtb := range p.PRTBs {
			if !prtb.Migrated {
				fmt.Fprintf(w, "  - migrating PRTB [%s]... ", prtb.Name)
				// if project is already migrated then we find out the new project name in the mgirated cluster

				prtb.Mutate(tc.Obj.Name, prtb.ProjectName)
				if err := client.ProjectRoleTemplateBindings.Create(ctx, prtb.ProjectName, prtb.Obj, nil, v1.CreateOptions{}); err != nil {
					return err
				}
				fmt.Fprintf(w, "Done.\n")
			}
		}
		for _, ns := range p.Namespaces {
			if !ns.Migrated {
				fmt.Fprintf(w, "  - migrating Namespace [%s]... ", ns.Name)
				ns.Mutate(tc.Obj.Name, ns.ProjectName)
				if _, err := tc.Client.Namespace.Create(ns.Obj); err != nil {
					return err
				}
				fmt.Fprintf(w, "Done.\n")
			}
		}
	}
	for _, crtb := range c.ToMigrate.CRTBs {
		if !crtb.Migrated {
			fmt.Fprintf(w, "- migrating CRTB [%s]... ", crtb.Name)
			crtb.Mutate(tc)
			if err := client.ClusterRoleTemplateBindings.Create(ctx, tc.Obj.Name, crtb.Obj, nil, v1.CreateOptions{}); err != nil {
				return err
			}
			fmt.Fprintf(w, "Done.\n")
		}
	}
	// catalog repos
	for _, repo := range c.ToMigrate.ClusterRepos {
		if !repo.Migrated {
			fmt.Fprintf(w, "- migrating catalog repo [%s]... ", repo.Name)
			repo.Mutate()
			if err := tc.Client.ClusterRepos.Create(ctx, tc.Obj.Name, repo.Obj, nil, v1.CreateOptions{}); err != nil {
				return err
			}
			fmt.Fprintf(w, "Done.\n")
		}
	}

	return nil
}

func NewProjectName(ctx context.Context, targetClusterName, oldProjectName string, client *client.Clients) (string, error) {
	var projects v3.ProjectList
	if err := client.Projects.List(ctx, targetClusterName, &projects, v1.ListOptions{}); err != nil {
		return "", err
	}
	for _, project := range projects.Items {
		if oldProjectName == project.Spec.DisplayName {
			return project.Name, nil
		}
	}
	return "", errors.New("failed to find project with the name " + oldProjectName)
}
