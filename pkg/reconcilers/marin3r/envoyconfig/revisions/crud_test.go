package revisions

import (
	"context"
	"reflect"
	"testing"

	marin3rv1alpha1 "github.com/3scale/marin3r/apis/marin3r/v1alpha1"
	"github.com/3scale/marin3r/pkg/reconcilers/marin3r/envoyconfig/filters"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/scheme"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var s *runtime.Scheme = scheme.Scheme

func init() {
	s.AddKnownTypes(marin3rv1alpha1.GroupVersion,
		&marin3rv1alpha1.EnvoyConfigRevision{},
		&marin3rv1alpha1.EnvoyConfigRevisionList{},
	)
}

func TestListRevisions(t *testing.T) {
	tests := []struct {
		name      string
		k8sClient client.Client
		namespace string
		filters   []filters.RevisionFilter
		wantCount int
		wantErr   bool
	}{
		{
			"Returns all EnvoyConfigRevisions for the nodeID",
			fake.NewFakeClientWithScheme(s,
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr1",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test"},
				}},
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr2",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test"},
				}},
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr3",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "other"},
				}},
			),
			"test",
			[]filters.RevisionFilter{filters.ByNodeID("test")},
			2,
			false,
		},
		{
			"Returns all EnvoyConfigRevisions for the nodeID and version",
			fake.NewFakeClientWithScheme(s,
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr1",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "1"},
				}},
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr2",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "2"},
				}},
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr3",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "3"},
				}},
			),
			"test",
			[]filters.RevisionFilter{filters.ByNodeID("test"), filters.ByVersion("1")},
			1,
			false,
		},
		{
			"Only returns revisions in the same Namespace",
			fake.NewFakeClientWithScheme(s,
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test"},
				}},
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr",
					Namespace: "other",
					Labels:    map[string]string{filters.NodeIDTag: "test"},
				}},
			),
			"test",
			[]filters.RevisionFilter{filters.ByNodeID("test")},
			1,
			false,
		},
		{
			"Returns an error if no revisions are found that match the provided filters",
			fake.NewFakeClientWithScheme(s),
			"test",
			[]filters.RevisionFilter{filters.ByNodeID("test")},
			0,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := List(context.TODO(), tt.k8sClient, tt.namespace, tt.filters...)
			if (err != nil) != tt.wantErr {
				t.Errorf("ListRevisions() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && len(got.Items) != tt.wantCount {
				t.Errorf("ListRevisions() = %v, want %v", len(got.Items), tt.wantCount)
			}
		})
	}
}

func TestGetRevision(t *testing.T) {
	tests := []struct {
		name      string
		k8sClient client.Client
		namespace string
		filters   []filters.RevisionFilter
		want      *marin3rv1alpha1.EnvoyConfigRevision
		wantErr   bool
	}{
		{
			"Returns all the EnvoyConfigRevision that matches the given filters",
			fake.NewFakeClientWithScheme(s,
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr1",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "1"},
				}},
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr2",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "2"},
				}},
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr3",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "3"},
				}},
			),
			"test",
			[]filters.RevisionFilter{filters.ByNodeID("test"), filters.ByVersion("1")},
			&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
				Name:      "ecr1",
				Namespace: "test",
				Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "1"},
			}},
			false,
		},
		{
			"Returns an error if API returns more than one EnvoyConfigRevision",
			fake.NewFakeClientWithScheme(s,
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr1",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "1"},
				}},
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr2",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "1"},
				}},
				&marin3rv1alpha1.EnvoyConfigRevision{ObjectMeta: metav1.ObjectMeta{
					Name:      "ecr3",
					Namespace: "test",
					Labels:    map[string]string{filters.NodeIDTag: "test", filters.VersionTag: "3"},
				}},
			),
			"test",
			[]filters.RevisionFilter{filters.ByNodeID("test"), filters.ByVersion("1")},
			nil,
			true,
		},
		{
			"Returns an error if API returns no EnvoyConfigRevision",
			fake.NewFakeClientWithScheme(s),
			"test",
			[]filters.RevisionFilter{filters.ByNodeID("test"), filters.ByVersion("1")},
			nil,
			true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {

			got, err := Get(context.TODO(), tt.k8sClient, tt.namespace, tt.filters...)
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRevision() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GetRevision() = %v, want %v", got, tt.want)
			}
		})
	}
}
