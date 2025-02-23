package reconcilers

import (
	"context"
	"crypto/x509"
	"reflect"
	"testing"
	"time"

	operatorv1alpha1 "github.com/3scale-ops/marin3r/apis/operator.marin3r/v1alpha1"
	"github.com/3scale-ops/marin3r/pkg/reconcilers/operator/discoveryservicecertificate/providers"
	"github.com/3scale-ops/marin3r/pkg/util/clock"
	"github.com/3scale-ops/marin3r/pkg/util/pki"
	"github.com/MakeNowJust/heredoc/v2"
	"github.com/go-logr/logr"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
)

var s *runtime.Scheme = scheme.Scheme

func init() {
	s.AddKnownTypes(operatorv1alpha1.GroupVersion,
		&operatorv1alpha1.DiscoveryServiceCertificate{},
	)
}

// testCertificateProvider fakes a certificate provider by returning the hardcoded certificates the provider
// is created with. The certificate in index 0 is returned when CreateCertificate() is invoked and its used as the
// "active" certificate for GetCertificate() and VerifyCertificate() operations. Each time UpdateCertificate()
// is called, the next certificates in the slice is used as the "active" one for all methods.
type testCertificateProvider struct {
	index        int
	certificates [][]byte
	currentTime  time.Time
}

func (tcp *testCertificateProvider) CreateCertificate() ([]byte, []byte, error) {
	tcp.index = 0
	cert := tcp.certificates[tcp.index]
	return cert, []byte("key"), nil
}
func (tcp *testCertificateProvider) GetCertificate() ([]byte, []byte, error) {
	if tcp.index < 0 {
		return []byte{}, []byte{}, errors.NewNotFound(schema.GroupResource{}, "test")
	}
	cert := tcp.certificates[tcp.index]
	return cert, []byte("key"), nil
}
func (tcp *testCertificateProvider) UpdateCertificate() ([]byte, []byte, error) {
	tcp.index = tcp.index + 1
	cert := tcp.certificates[tcp.index]
	return cert, []byte("key"), nil
}

func (tcp *testCertificateProvider) VerifyCertificate() error {
	var cert *x509.Certificate
	cert, err := pki.LoadX509Certificate(tcp.certificates[tcp.index])
	if err != nil {
		return err
	}

	roots := x509.NewCertPool()
	roots.AddCert(cert)

	opts := x509.VerifyOptions{
		Roots:       roots,
		CurrentTime: tcp.currentTime,
	}

	_, err = cert.Verify(opts)
	if err != nil {
		return pki.NewVerifyError(err.Error())
	}

	return nil
}

func TestNewCertificateReconciler(t *testing.T) {
	type args struct {
		ctx      context.Context
		logger   logr.Logger
		client   client.Client
		s        *runtime.Scheme
		dsc      *operatorv1alpha1.DiscoveryServiceCertificate
		provider providers.CertificateProvider
	}
	tests := []struct {
		name string
		args args
		want CertificateReconciler
	}{
		{
			name: "Returns a CertificateReconciler",
			args: args{
				ctx:      context.TODO(),
				logger:   ctrl.Log.WithName("test"),
				client:   fake.NewClientBuilder().WithScheme(s).Build(),
				s:        s,
				dsc:      &operatorv1alpha1.DiscoveryServiceCertificate{},
				provider: &testCertificateProvider{},
			},
			want: CertificateReconciler{
				ctx:      context.TODO(),
				logger:   ctrl.Log.WithName("test"),
				client:   fake.NewClientBuilder().WithScheme(s).Build(),
				scheme:   s,
				dsc:      &operatorv1alpha1.DiscoveryServiceCertificate{},
				provider: &testCertificateProvider{},
				clock:    clock.Real{},
				ready:    false,
				hash:     "",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := NewCertificateReconciler(tt.args.ctx, tt.args.logger, tt.args.client, tt.args.s, tt.args.dsc, tt.args.provider); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("NewCertificateReconciler() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCertificateReconciler_IsReady(t *testing.T) {
	tests := []struct {
		name string
		r    *CertificateReconciler
		want bool
	}{
		{
			name: "Returns r.ready",
			r: &CertificateReconciler{
				ctx:    context.TODO(),
				logger: ctrl.Log.WithName("test"),
				client: fake.NewClientBuilder().WithScheme(s).Build(),
				scheme: s,
				dsc:    &operatorv1alpha1.DiscoveryServiceCertificate{},
				ready:  true,
			},
			want: true,
		}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.IsReady(); got != tt.want {
				t.Errorf("CertificateReconciler.IsReady() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCertificateReconciler_GetCertificateHash(t *testing.T) {
	tests := []struct {
		name string
		r    *CertificateReconciler
		want string
	}{
		{
			name: "Returns r.hash",
			r: &CertificateReconciler{
				ctx:    context.TODO(),
				logger: ctrl.Log.WithName("test"),
				client: fake.NewClientBuilder().WithScheme(s).Build(),
				scheme: s,
				dsc:    &operatorv1alpha1.DiscoveryServiceCertificate{},
				hash:   "xxxx",
			},
			want: "xxxx",
		}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.r.GetCertificateHash(); got != tt.want {
				t.Errorf("CertificateReconciler.GetCertificateHash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestCertificateReconciler_Reconcile(t *testing.T) {
	tests := []struct {
		name          string
		r             *CertificateReconciler
		want          ctrl.Result
		wantErr       bool
		wantIsReady   bool
		wantHash      string
		wantNotBefore *time.Time
		wantNotAfter  *time.Time
		wantSchedule  *time.Duration
	}{
		{
			name: "Creates a new certificate",
			r: &CertificateReconciler{
				ctx:    context.TODO(),
				logger: ctrl.Log.WithName("test"),
				client: fake.NewClientBuilder().WithScheme(s).Build(),
				scheme: s,
				dsc: &operatorv1alpha1.DiscoveryServiceCertificate{
					ObjectMeta: metav1.ObjectMeta{Name: "dsc", Namespace: "test"},
					Spec:       operatorv1alpha1.DiscoveryServiceCertificateSpec{}},
				provider: &testCertificateProvider{
					index: -1,
					// go run hack/gen_cert.go --not-before=2021-01-01T00:00:00Z --not-after=2021-01-01T00:01:40Z --key-size 512
					certificates: [][]byte{
						[]byte(heredoc.Doc(`
						-----BEGIN CERTIFICATE-----
						MIIBdjCCASCgAwIBAgIQFS94k33VgPtanU/j0OvC8DANBgkqhkiG9w0BAQsFADAr
						MRUwEwYDVQQKEwxtYXJpbjNyLnRlc3QxEjAQBgNVBAMTCWxvY2FsaG9zdDAeFw0y
						MTAxMDEwMDAwMDBaFw0yMTAxMDEwMDAxNDBaMCsxFTATBgNVBAoTDG1hcmluM3Iu
						dGVzdDESMBAGA1UEAxMJbG9jYWxob3N0MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJB
						AK1ShFw1t1r8vrn5cVJj98ei4UYAwIy7hymr7oCXom1TcWCLURZsMfKG2A8YKUBC
						iKQWT/zAknqKOrV8qn9bSUkCAwEAAaMgMB4wDgYDVR0PAQH/BAQDAgWgMAwGA1Ud
						EwEB/wQCMAAwDQYJKoZIhvcNAQELBQADQQBVv03X7BjjcTqpkcCCiejTyJYTc1pN
						kfwbx8mNF+Zx5V763W74/+fr2Z5+Q0l7O1k3gcsnaWSoGfV9PST7iNpQ
						-----END CERTIFICATE-----
						`)),
					},
				},
				clock: clock.Real{},
			},
			want:          ctrl.Result{Requeue: true},
			wantErr:       false,
			wantIsReady:   false,
			wantHash:      "",
			wantNotBefore: nil,
			wantNotAfter:  nil,
			wantSchedule:  nil,
		},
		{
			name: "Verifies a certificate, schedules renewal",
			r: &CertificateReconciler{
				ctx:    context.TODO(),
				logger: ctrl.Log.WithName("test"),
				client: fake.NewClientBuilder().WithScheme(s).Build(),
				scheme: s,
				dsc: &operatorv1alpha1.DiscoveryServiceCertificate{
					ObjectMeta: metav1.ObjectMeta{Name: "dsc", Namespace: "test"},
					Spec:       operatorv1alpha1.DiscoveryServiceCertificateSpec{}},
				provider: &testCertificateProvider{
					index: 0,
					// go run hack/gen_cert.go --not-before=2021-01-01T00:00:00Z --not-after=2021-01-01T00:01:40Z --key-size 512
					certificates: [][]byte{
						[]byte(heredoc.Doc(`
						-----BEGIN CERTIFICATE-----
						MIIBdjCCASCgAwIBAgIQFS94k33VgPtanU/j0OvC8DANBgkqhkiG9w0BAQsFADAr
						MRUwEwYDVQQKEwxtYXJpbjNyLnRlc3QxEjAQBgNVBAMTCWxvY2FsaG9zdDAeFw0y
						MTAxMDEwMDAwMDBaFw0yMTAxMDEwMDAxNDBaMCsxFTATBgNVBAoTDG1hcmluM3Iu
						dGVzdDESMBAGA1UEAxMJbG9jYWxob3N0MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJB
						AK1ShFw1t1r8vrn5cVJj98ei4UYAwIy7hymr7oCXom1TcWCLURZsMfKG2A8YKUBC
						iKQWT/zAknqKOrV8qn9bSUkCAwEAAaMgMB4wDgYDVR0PAQH/BAQDAgWgMAwGA1Ud
						EwEB/wQCMAAwDQYJKoZIhvcNAQELBQADQQBVv03X7BjjcTqpkcCCiejTyJYTc1pN
						kfwbx8mNF+Zx5V763W74/+fr2Z5+Q0l7O1k3gcsnaWSoGfV9PST7iNpQ
						-----END CERTIFICATE-----
						`)),
					},
					currentTime: func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:01:00Z"); return t }(),
				},
				clock: clock.NewTest(func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:01:00Z"); return t }()),
			},
			want:          ctrl.Result{},
			wantErr:       false,
			wantIsReady:   true,
			wantHash:      "5c78c58c76",
			wantNotBefore: func() *time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:00:00Z"); return &t }(),
			wantNotAfter:  func() *time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:01:40Z"); return &t }(),
			wantSchedule:  func() *time.Duration { d := time.Duration(20 * time.Second); return &d }(),
		},
		{
			name: "Verifies a certificate, renewal disabled",
			r: &CertificateReconciler{
				ctx:    context.TODO(),
				logger: ctrl.Log.WithName("test"),
				client: fake.NewClientBuilder().WithScheme(s).Build(),
				scheme: s,
				dsc: &operatorv1alpha1.DiscoveryServiceCertificate{
					ObjectMeta: metav1.ObjectMeta{Name: "dsc", Namespace: "test"},
					Spec: operatorv1alpha1.DiscoveryServiceCertificateSpec{
						CertificateRenewalConfig: &operatorv1alpha1.CertificateRenewalConfig{Enabled: false},
					}},
				provider: &testCertificateProvider{
					index: 0,
					// go run hack/gen_cert.go --not-before=2021-01-01T00:00:00Z --not-after=2021-01-01T00:01:40Z --key-size 512
					certificates: [][]byte{
						[]byte(heredoc.Doc(`
						-----BEGIN CERTIFICATE-----
						MIIBdjCCASCgAwIBAgIQFS94k33VgPtanU/j0OvC8DANBgkqhkiG9w0BAQsFADAr
						MRUwEwYDVQQKEwxtYXJpbjNyLnRlc3QxEjAQBgNVBAMTCWxvY2FsaG9zdDAeFw0y
						MTAxMDEwMDAwMDBaFw0yMTAxMDEwMDAxNDBaMCsxFTATBgNVBAoTDG1hcmluM3Iu
						dGVzdDESMBAGA1UEAxMJbG9jYWxob3N0MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJB
						AK1ShFw1t1r8vrn5cVJj98ei4UYAwIy7hymr7oCXom1TcWCLURZsMfKG2A8YKUBC
						iKQWT/zAknqKOrV8qn9bSUkCAwEAAaMgMB4wDgYDVR0PAQH/BAQDAgWgMAwGA1Ud
						EwEB/wQCMAAwDQYJKoZIhvcNAQELBQADQQBVv03X7BjjcTqpkcCCiejTyJYTc1pN
						kfwbx8mNF+Zx5V763W74/+fr2Z5+Q0l7O1k3gcsnaWSoGfV9PST7iNpQ
						-----END CERTIFICATE-----
						`)),
					},
					currentTime: func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:01:00Z"); return t }(),
				},
				clock: clock.NewTest(func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:01:00Z"); return t }()),
			},
			want:          ctrl.Result{},
			wantErr:       false,
			wantIsReady:   true,
			wantHash:      "5c78c58c76",
			wantNotBefore: func() *time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:00:00Z"); return &t }(),
			wantNotAfter:  func() *time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:01:40Z"); return &t }(),
			wantSchedule:  func() *time.Duration { d := time.Duration(41 * time.Second); return &d }(),
		},
		{
			name: "Returns not ready on verify error, renewal disabled",
			r: &CertificateReconciler{
				ctx:    context.TODO(),
				logger: ctrl.Log.WithName("test"),
				client: fake.NewClientBuilder().WithScheme(s).Build(),
				scheme: s,
				dsc: &operatorv1alpha1.DiscoveryServiceCertificate{
					ObjectMeta: metav1.ObjectMeta{Name: "dsc", Namespace: "test"},
					Spec: operatorv1alpha1.DiscoveryServiceCertificateSpec{
						CertificateRenewalConfig: &operatorv1alpha1.CertificateRenewalConfig{Enabled: false},
					}},
				provider: &testCertificateProvider{
					index: 0,
					// go run hack/gen_cert.go --not-before=2021-01-01T00:00:00Z --not-after=2021-01-01T00:01:40Z --key-size 512
					certificates: [][]byte{
						[]byte(heredoc.Doc(`
						-----BEGIN CERTIFICATE-----
						MIIBdjCCASCgAwIBAgIQFS94k33VgPtanU/j0OvC8DANBgkqhkiG9w0BAQsFADAr
						MRUwEwYDVQQKEwxtYXJpbjNyLnRlc3QxEjAQBgNVBAMTCWxvY2FsaG9zdDAeFw0y
						MTAxMDEwMDAwMDBaFw0yMTAxMDEwMDAxNDBaMCsxFTATBgNVBAoTDG1hcmluM3Iu
						dGVzdDESMBAGA1UEAxMJbG9jYWxob3N0MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJB
						AK1ShFw1t1r8vrn5cVJj98ei4UYAwIy7hymr7oCXom1TcWCLURZsMfKG2A8YKUBC
						iKQWT/zAknqKOrV8qn9bSUkCAwEAAaMgMB4wDgYDVR0PAQH/BAQDAgWgMAwGA1Ud
						EwEB/wQCMAAwDQYJKoZIhvcNAQELBQADQQBVv03X7BjjcTqpkcCCiejTyJYTc1pN
						kfwbx8mNF+Zx5V763W74/+fr2Z5+Q0l7O1k3gcsnaWSoGfV9PST7iNpQ
						-----END CERTIFICATE-----
						`)),
					},
					currentTime: func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:02:00Z"); return t }(),
				},
				clock: clock.NewTest(func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:02:00Z"); return t }()),
			},
			want:          ctrl.Result{},
			wantErr:       false,
			wantIsReady:   false,
			wantHash:      "5c78c58c76",
			wantNotBefore: func() *time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:00:00Z"); return &t }(),
			wantNotAfter:  func() *time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:01:40Z"); return &t }(),
			wantSchedule:  nil,
		},
		{
			name: "Renewes certificate when it is expired (renewal enabled)",
			r: &CertificateReconciler{
				ctx:    context.TODO(),
				logger: ctrl.Log.WithName("test"),
				client: fake.NewClientBuilder().WithScheme(s).Build(),
				scheme: s,
				dsc: &operatorv1alpha1.DiscoveryServiceCertificate{
					ObjectMeta: metav1.ObjectMeta{Name: "dsc", Namespace: "test"},
					Spec:       operatorv1alpha1.DiscoveryServiceCertificateSpec{},
				},
				provider: &testCertificateProvider{
					index: 0,
					// go run hack/gen_cert.go --not-before=2021-01-01T00:00:00Z --not-after=2021-01-01T00:01:40Z --key-size 512
					certificates: [][]byte{
						[]byte(heredoc.Doc(`
						-----BEGIN CERTIFICATE-----
						MIIBdjCCASCgAwIBAgIQFS94k33VgPtanU/j0OvC8DANBgkqhkiG9w0BAQsFADAr
						MRUwEwYDVQQKEwxtYXJpbjNyLnRlc3QxEjAQBgNVBAMTCWxvY2FsaG9zdDAeFw0y
						MTAxMDEwMDAwMDBaFw0yMTAxMDEwMDAxNDBaMCsxFTATBgNVBAoTDG1hcmluM3Iu
						dGVzdDESMBAGA1UEAxMJbG9jYWxob3N0MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJB
						AK1ShFw1t1r8vrn5cVJj98ei4UYAwIy7hymr7oCXom1TcWCLURZsMfKG2A8YKUBC
						iKQWT/zAknqKOrV8qn9bSUkCAwEAAaMgMB4wDgYDVR0PAQH/BAQDAgWgMAwGA1Ud
						EwEB/wQCMAAwDQYJKoZIhvcNAQELBQADQQBVv03X7BjjcTqpkcCCiejTyJYTc1pN
						kfwbx8mNF+Zx5V763W74/+fr2Z5+Q0l7O1k3gcsnaWSoGfV9PST7iNpQ
						-----END CERTIFICATE-----
						`)),
						// go run hack/gen_cert.go --not-before=2021-01-01T00:02:00Z --not-after=2021-01-01T00:03:40Z --key-size 512
						[]byte(heredoc.Doc(`
						-----BEGIN CERTIFICATE-----
						MIIBdjCCASCgAwIBAgIQPLCk1wrD/xwhGeYY+8PyXzANBgkqhkiG9w0BAQsFADAr
						MRUwEwYDVQQKEwxtYXJpbjNyLnRlc3QxEjAQBgNVBAMTCWxvY2FsaG9zdDAeFw0y
						MTAxMDEwMDAyMDBaFw0yMTAxMDEwMDAzNDBaMCsxFTATBgNVBAoTDG1hcmluM3Iu
						dGVzdDESMBAGA1UEAxMJbG9jYWxob3N0MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJB
						ALar6qCiHa0rU/FYLrfp0AxWncC2cPcrbWeAg0fl9sj9i7pPUnWKwDtPtF7XOVbr
						IJNLS2eiVwY51t33ZzJSJ0cCAwEAAaMgMB4wDgYDVR0PAQH/BAQDAgWgMAwGA1Ud
						EwEB/wQCMAAwDQYJKoZIhvcNAQELBQADQQBX9jIA4PgYKa4O1GAC95xXYkPQtwWJ
						GLdoN+PINhm0k1dg/nzRYQrefXlkju3o98iSUvi9RjjTT2xeW9LIiBUo
						-----END CERTIFICATE-----
						`)),
					},
					currentTime: func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:02:00Z"); return t }(),
				},
				clock: clock.NewTest(func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:02:00Z"); return t }()),
			},
			want:        ctrl.Result{Requeue: true},
			wantErr:     false,
			wantIsReady: false,
		},
		{
			name: "Renewes certificate when within renewal window (renewal enabled)",
			r: &CertificateReconciler{
				ctx:    context.TODO(),
				logger: ctrl.Log.WithName("test"),
				client: fake.NewClientBuilder().WithScheme(s).Build(),
				scheme: s,
				dsc: &operatorv1alpha1.DiscoveryServiceCertificate{
					ObjectMeta: metav1.ObjectMeta{Name: "dsc", Namespace: "test"},
					Spec:       operatorv1alpha1.DiscoveryServiceCertificateSpec{},
				},
				provider: &testCertificateProvider{
					index: 0,
					// go run hack/gen_cert.go --not-before=2021-01-01T00:00:00Z --not-after=2021-01-01T00:01:40Z --key-size 512
					certificates: [][]byte{
						[]byte(heredoc.Doc(`
						-----BEGIN CERTIFICATE-----
						MIIBdjCCASCgAwIBAgIQFS94k33VgPtanU/j0OvC8DANBgkqhkiG9w0BAQsFADAr
						MRUwEwYDVQQKEwxtYXJpbjNyLnRlc3QxEjAQBgNVBAMTCWxvY2FsaG9zdDAeFw0y
						MTAxMDEwMDAwMDBaFw0yMTAxMDEwMDAxNDBaMCsxFTATBgNVBAoTDG1hcmluM3Iu
						dGVzdDESMBAGA1UEAxMJbG9jYWxob3N0MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJB
						AK1ShFw1t1r8vrn5cVJj98ei4UYAwIy7hymr7oCXom1TcWCLURZsMfKG2A8YKUBC
						iKQWT/zAknqKOrV8qn9bSUkCAwEAAaMgMB4wDgYDVR0PAQH/BAQDAgWgMAwGA1Ud
						EwEB/wQCMAAwDQYJKoZIhvcNAQELBQADQQBVv03X7BjjcTqpkcCCiejTyJYTc1pN
						kfwbx8mNF+Zx5V763W74/+fr2Z5+Q0l7O1k3gcsnaWSoGfV9PST7iNpQ
						-----END CERTIFICATE-----
						`)),
						// go run hack/gen_cert.go --not-before=2021-01-01T00:02:00Z --not-after=2021-01-01T00:03:40Z --key-size 512
						[]byte(heredoc.Doc(`
						-----BEGIN CERTIFICATE-----
						MIIBdjCCASCgAwIBAgIQPLCk1wrD/xwhGeYY+8PyXzANBgkqhkiG9w0BAQsFADAr
						MRUwEwYDVQQKEwxtYXJpbjNyLnRlc3QxEjAQBgNVBAMTCWxvY2FsaG9zdDAeFw0y
						MTAxMDEwMDAyMDBaFw0yMTAxMDEwMDAzNDBaMCsxFTATBgNVBAoTDG1hcmluM3Iu
						dGVzdDESMBAGA1UEAxMJbG9jYWxob3N0MFwwDQYJKoZIhvcNAQEBBQADSwAwSAJB
						ALar6qCiHa0rU/FYLrfp0AxWncC2cPcrbWeAg0fl9sj9i7pPUnWKwDtPtF7XOVbr
						IJNLS2eiVwY51t33ZzJSJ0cCAwEAAaMgMB4wDgYDVR0PAQH/BAQDAgWgMAwGA1Ud
						EwEB/wQCMAAwDQYJKoZIhvcNAQELBQADQQBX9jIA4PgYKa4O1GAC95xXYkPQtwWJ
						GLdoN+PINhm0k1dg/nzRYQrefXlkju3o98iSUvi9RjjTT2xeW9LIiBUo
						-----END CERTIFICATE-----
						`)),
					},
					currentTime: func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:01:21Z"); return t }(),
				},
				clock:     clock.NewTest(func() time.Time { t, _ := time.Parse(time.RFC3339, "2021-01-01T00:01:21Z"); return t }()),
				ready:     true,
				hash:      "",
				notBefore: &time.Time{},
				notAfter:  &time.Time{},
				schedule:  nil,
			},
			want:          ctrl.Result{Requeue: true},
			wantErr:       false,
			wantIsReady:   true,
			wantNotBefore: &time.Time{},
			wantNotAfter:  &time.Time{},
			wantSchedule:  nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tt.r.Reconcile()
			if (err != nil) != tt.wantErr {
				t.Errorf("CertificateReconciler.Reconcile() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("CertificateReconciler.Reconcile() = %v, want %v", got, tt.want)
			}
			if tt.wantIsReady != tt.r.IsReady() {
				t.Errorf("CertificateReconciler.Reconcile() IsReady = %v, want %v", tt.r.IsReady(), tt.wantIsReady)
			}

			if tt.wantIsReady {
				if tt.wantHash != tt.r.GetCertificateHash() {
					t.Errorf("CertificateReconciler.Reconcile() GetCertificateHash = %v, want %v", tt.r.GetCertificateHash(), tt.wantHash)
				}
				if *tt.wantNotBefore != tt.r.NotBefore() {
					t.Errorf("CertificateReconciler.Reconcile() NotBefore = %v, want %v", tt.r.NotBefore(), *tt.wantNotBefore)
				}
				if *tt.wantNotAfter != tt.r.NotAfter() {
					t.Errorf("CertificateReconciler.Reconcile() NotAfter = %v, want %v", tt.r.NotAfter(), *tt.wantNotAfter)
				}
				if tt.wantSchedule != nil && *tt.wantSchedule != *tt.r.GetSchedule() {
					t.Errorf("CertificateReconciler.Reconcile() GetSchedule = %v, want %v", tt.r.GetSchedule(), tt.wantSchedule)
				}
			}
		})
	}

}
