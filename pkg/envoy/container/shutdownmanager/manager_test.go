package shutdownmanager

import (
	"context"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/MakeNowJust/heredoc/v2"
	"github.com/phayes/freeport"
)

func TestManager_prometheusURL(t *testing.T) {
	tests := []struct {
		name string
		mgr  Manager
		want string
	}{
		{
			name: "Returns the metrics url",
			mgr:  Manager{EnvoyAdminAddress: "http://localhost:2000"},
			want: "http://localhost:2000/stats/prometheus",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mgr.envoyPrometheusURL(); got != tt.want {
				t.Errorf("Manager.prometheusURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManager_drainURL(t *testing.T) {
	tests := []struct {
		name string
		mgr  Manager
		want string
	}{
		{
			name: "Returns the drain url",
			mgr:  Manager{EnvoyAdminAddress: "http://localhost:2000"},
			want: "http://localhost:2000/healthcheck/fail",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mgr.envoyShutdownURL(); got != tt.want {
				t.Errorf("Manager.drainURL() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestManager_Start(t *testing.T) {
	tests := []struct {
		name    string
		mgr     Manager
		want    string
		wantErr bool
	}{
		{
			name:    "Fails (tries to bind to privileged port)",
			mgr:     Manager{HTTPServePort: 80},
			want:    "http://localhost:2000/healthcheck/fail",
			wantErr: true,
		},
		{
			name:    "Runs server and closes cleanly",
			mgr:     Manager{HTTPServePort: 5000},
			want:    "http://localhost:2000/healthcheck/fail",
			wantErr: false,
		},
	}

	for _, tt := range tests {
		ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
		defer cancel()

		t.Run(tt.name, func(t *testing.T) {
			if err := tt.mgr.Start(ctx); err != nil && !tt.wantErr {
				t.Errorf("Manager.Start() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManager_healthzHandler(t *testing.T) {
	mgr := Manager{}
	req := httptest.NewRequest("GET", HealthEndpoint, nil)
	rr := httptest.NewRecorder()
	mgr.healthzHandler(rr, req)

	if rr.Code != http.StatusOK {
		t.Errorf("Manager.healthzHandler() status code = %v, want %v", rr.Code, http.StatusOK)
	}
}

func TestManager_waitForDrainHandler(t *testing.T) {
	mgr := Manager{ShutdownReadyCheckInterval: 10 * time.Millisecond}

	ctx, cancel := context.WithTimeout(context.Background(), time.Millisecond*100)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, "GET", ShutdownEndpoint, nil)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("Returns HTTP 200 when file exists", func(t *testing.T) {

		tmpdir, err := ioutil.TempDir("", "shutdownmanager_test-*")
		defer os.RemoveAll(tmpdir)
		if err != nil {
			t.Error(err)
		}
		mgr.ShutdownReadyFile = path.Join(tmpdir, "ok")

		go func() {
			time.Sleep(50 * time.Millisecond)
			file, err := os.Create(mgr.ShutdownReadyFile)
			if err != nil {
				t.Error(err)
			}
			defer file.Close()
		}()

		rr := httptest.NewRecorder()
		http.HandlerFunc(mgr.shutdownHandler).ServeHTTP(rr, req)

		if rr.Code != http.StatusOK {
			t.Errorf("Manager.waitForDrainHandler() status code = %v, want %v", rr.Code, http.StatusOK)
		}
	})

	t.Run("Request canceled", func(t *testing.T) {
		rr := httptest.NewRecorder()
		http.HandlerFunc(mgr.shutdownHandler).ServeHTTP(rr, req)
	})

}

func TestManager_drainHandler(t *testing.T) {
	mgr := Manager{
		ShutdownReadyFile:          "ok",
		ShutdownReadyCheckInterval: 10 * time.Millisecond,
		CheckDrainInterval:         10 * time.Millisecond,
		CheckDrainDelay:            0,
		StartDrainDelay:            0,
	}

	run := func(mgr Manager, drainHandler, statsHandler http.HandlerFunc) error {
		tmpdir, err := ioutil.TempDir("", "shutdownmanager_test-*")
		defer os.RemoveAll(tmpdir)
		if err != nil {
			t.Error(err)
		}
		mgr.ShutdownReadyFile = path.Join(tmpdir, "ok")

		port, err := freeport.GetFreePort()
		if err != nil {
			t.Fatal(err)
		}
		mgr.EnvoyAdminAddress = fmt.Sprintf("http://localhost:%d", port)

		// Create a request against shutdown manager /drain endpoint
		ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, "GET", DrainEndpoint, nil)
		if err != nil {
			t.Fatal(err)
		}
		rr := httptest.NewRecorder()

		// Start a mock envoy server
		go func() {
			mockServer(port, map[string]http.HandlerFunc{
				envoyShutdownEndpoint:   drainHandler,
				envoyPrometheusEndpoint: statsHandler,
			})
		}()

		// Make a request to /drain in a goroutine
		success := make(chan struct{})
		go func() {
			mgr.drainHandler(rr, req)
			if rr.Result().StatusCode == http.StatusOK {
				close(success)
			}
		}()

		// Block until success or timeout
		select {
		case <-ctx.Done():
			return fmt.Errorf("request timed out")
		case <-success:
			return nil
		}

	}

	t.Run("Request should timeout (no metrics returned)", func(t *testing.T) {
		mgr.MinOpenConnections = 0
		err := run(mgr,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		)

		if err == nil {
			t.Errorf("Manager.drainHandler() error was expected, had success")
		}
	})

	t.Run("Request should succeed (metrics return 0 connections, min = 0)", func(t *testing.T) {
		mgr.MinOpenConnections = 0
		err := run(mgr,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(heredoc.Doc(`
					# TYPE envoy_http_downstream_cx_active gauge
					envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_listener"} 0
				`)))
			}),
		)

		if err != nil {
			t.Errorf("Manager.drainHandler() error = %s", err)
		}
	})

	t.Run("Request should succeed (metrics return 5 connections, min = 5)", func(t *testing.T) {
		mgr.MinOpenConnections = 5
		err := run(mgr,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(heredoc.Doc(`
					# TYPE envoy_http_downstream_cx_active gauge
					envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_listener"} 0
					envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="aaaa"} 3
					envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="bbbb"} 2
				`)))
			}),
		)

		if err != nil {
			t.Errorf("Manager.drainHandler() error = %s", err)
		}
	})

	t.Run("Request should time out (metrics return 5 connections, min = 2)", func(t *testing.T) {
		mgr.MinOpenConnections = 2
		err := run(mgr,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(heredoc.Doc(`
					# TYPE envoy_http_downstream_cx_active gauge
					envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_listener"} 0
					envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="aaaa"} 3
					envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="bbbb"} 2
				`)))
			}),
		)

		if err == nil {
			t.Errorf("Manager.drainHandler() error was expected, had success")
		}
	})

	t.Run("Request should succeed (metrics return 3 admin connections, min = 0)", func(t *testing.T) {
		mgr.MinOpenConnections = 0
		err := run(mgr,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("OK")) }),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Write([]byte(heredoc.Doc(`
					# TYPE envoy_http_downstream_cx_active gauge
					envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_listener"} 0
					envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="admin"} 3
				`)))
			}),
		)

		if err != nil {
			t.Errorf("Manager.drainHandler() error = %s", err)
		}
	})

	t.Run("Request should time out (drain endpoint fails)", func(t *testing.T) {
		mgr.MinOpenConnections = 0
		err := run(mgr,
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "error", http.StatusInternalServerError)
			}),
			http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}),
		)

		if err == nil {
			t.Errorf("Manager.drainHandler() error was expected, had success")
		}
	})
}

func TestManager_shutdownEnvoy(t *testing.T) {
	type fields struct {
		HTTPServePort              int
		ShutdownReadyFile          string
		ShutdownReadyCheckInterval time.Duration
		CheckDrainInterval         time.Duration
		CheckDrainDelay            time.Duration
		StartDrainDelay            time.Duration
		EnvoyAdminURL              string
		MinOpenConnections         int
	}
	tests := []struct {
		name    string
		fields  fields
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &Manager{
				HTTPServePort:              tt.fields.HTTPServePort,
				ShutdownReadyFile:          tt.fields.ShutdownReadyFile,
				ShutdownReadyCheckInterval: tt.fields.ShutdownReadyCheckInterval,
				CheckDrainInterval:         tt.fields.CheckDrainInterval,
				CheckDrainDelay:            tt.fields.CheckDrainDelay,
				StartDrainDelay:            tt.fields.StartDrainDelay,
				EnvoyAdminAddress:          tt.fields.EnvoyAdminURL,
				MinOpenConnections:         tt.fields.MinOpenConnections,
			}
			if err := mgr.shutdownEnvoy(); (err != nil) != tt.wantErr {
				t.Errorf("Manager.shutdownEnvoy() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestManager_getOpenConnections(t *testing.T) {
	type fields struct {
		HTTPServePort              int
		ShutdownReadyFile          string
		ShutdownReadyCheckInterval time.Duration
		CheckDrainInterval         time.Duration
		CheckDrainDelay            time.Duration
		StartDrainDelay            time.Duration
		EnvoyAdminURL              string
		MinOpenConnections         int
	}
	tests := []struct {
		name    string
		fields  fields
		want    int
		wantErr bool
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mgr := &Manager{
				HTTPServePort:              tt.fields.HTTPServePort,
				ShutdownReadyFile:          tt.fields.ShutdownReadyFile,
				ShutdownReadyCheckInterval: tt.fields.ShutdownReadyCheckInterval,
				CheckDrainInterval:         tt.fields.CheckDrainInterval,
				CheckDrainDelay:            tt.fields.CheckDrainDelay,
				StartDrainDelay:            tt.fields.StartDrainDelay,
				EnvoyAdminAddress:          tt.fields.EnvoyAdminURL,
				MinOpenConnections:         tt.fields.MinOpenConnections,
			}
			got, err := mgr.getOpenConnections()
			if (err != nil) != tt.wantErr {
				t.Errorf("Manager.getOpenConnections() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("Manager.getOpenConnections() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_parseOpenConnections(t *testing.T) {
	tests := []struct {
		name    string
		mgr     Manager
		stats   io.Reader
		want    int
		wantErr bool
	}{
		{
			name: "Returns 0",
			mgr:  Manager{},
			stats: strings.NewReader(heredoc.Doc(`
				# TYPE envoy_http_downstream_cx_active gauge
				envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_listener"} 0
				envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_other_listener"} 0
			`)),
			want:    0,
			wantErr: false,
		},
		{
			name: "Returns 9",
			mgr:  Manager{},
			stats: strings.NewReader(heredoc.Doc(`
				# TYPE envoy_http_downstream_cx_active gauge
				envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_listener"} 5
				envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_other_listener"} 4
			`)),
			want:    9,
			wantErr: false,
		},
		{
			name: "Returns 9 (ignores admin listener connections)",
			mgr:  Manager{},
			stats: strings.NewReader(heredoc.Doc(`
				# TYPE envoy_http_downstream_cx_active gauge
				envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_listener"} 5
				envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="my_other_listener"} 4
				envoy_http_downstream_cx_active{envoy_http_conn_manager_prefix="admin"} 3
			`)),
			want:    9,
			wantErr: false,
		},
		{
			name: "Returns error (missing metric)",
			mgr:  Manager{},
			stats: strings.NewReader(heredoc.Doc(`
				# TYPE envoy_listener_admin_http_downstream_rq_completed counter
				envoy_listener_admin_http_downstream_rq_completed{envoy_http_conn_manager_prefix="admin"} 0
				# TYPE envoy_listener_admin_http_downstream_rq_xx counter
				envoy_listener_admin_http_downstream_rq_xx{envoy_response_code_class="1",envoy_http_conn_manager_prefix="admin"} 0
				envoy_listener_admin_http_downstream_rq_xx{envoy_response_code_class="2",envoy_http_conn_manager_prefix="admin"} 0
				envoy_listener_admin_http_downstream_rq_xx{envoy_response_code_class="3",envoy_http_conn_manager_prefix="admin"} 0
				envoy_listener_admin_http_downstream_rq_xx{envoy_response_code_class="4",envoy_http_conn_manager_prefix="admin"} 0
				envoy_listener_admin_http_downstream_rq_xx{envoy_response_code_class="5",envoy_http_conn_manager_prefix="admin"} 0
			`)),
			want:    -1,
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseOpenConnections(tt.stats)
			if (err != nil) != tt.wantErr {
				t.Errorf("parseOpenConnections() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("parseOpenConnections() = %v, want %v", got, tt.want)
			}
		})
	}
}

func mockServer(port int, handlers map[string]http.HandlerFunc) {

	mux := http.NewServeMux()
	srv := http.Server{Addr: fmt.Sprintf(":%d", port), Handler: mux}
	for pattern, handler := range handlers {
		mux.HandleFunc(pattern, handler)
	}

	srv.ListenAndServe()
}
