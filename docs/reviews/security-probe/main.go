// Reproductions for the 2026-09-10 audit. These assert observed vulnerabilities,
// not desired regression-test behavior. Run: go run ./docs/reviews/security-probe
package main

import (
 "context"
 "encoding/json"
 "fmt"
 "io"
 "log/slog"
 "net/http"
 "os"
 "path/filepath"
 "strings"
 "time"

 "github.com/mockagents/mockagents/internal/engine"
 "github.com/mockagents/mockagents/internal/engine/state"
 "github.com/mockagents/mockagents/internal/metrics"
 "github.com/mockagents/mockagents/internal/recording"
 "github.com/mockagents/mockagents/internal/server"
 "github.com/mockagents/mockagents/internal/tenancy"
)

func must(err error) { if err != nil { panic(err) } }
func main() {
 ctx := context.Background()
 dir, err := os.MkdirTemp("", "mockagents-security-probe-"); must(err)
 defer os.RemoveAll(dir)
 store, err := tenancy.NewSQLiteStore(filepath.Join(dir,"tenancy.db")); must(err)
 defer store.Close()
 tenant, err := store.CreateTenant(ctx,"default"); must(err)
 platform, err := store.CreateAPIKey(ctx,tenant.ID,"bootstrap-admin",tenancy.RolePlatform); must(err)
 admin, err := store.CreateAPIKey(ctx,tenant.ID,"delegated-admin",tenancy.RoleAdmin); must(err)
 viewer, err := store.CreateAPIKey(ctx,tenant.ID,"viewer",tenancy.RoleViewer); must(err)
 log := slog.New(slog.NewTextHandler(io.Discard,nil))
 reg := engine.NewAgentRegistry()
 eng := engine.NewEngine(reg,state.NewMemoryStore(time.Minute),log)
 cfg := server.DefaultConfig(); cfg.Port=0; cfg.TenancyStore=store; cfg.Metrics=metrics.New("audit")
 cfg.Metrics.RecordRequest("openai","other-tenant-confidential-agent",200,time.Millisecond)
 srv := server.New(eng,cfg,log); must(srv.Listen()); go srv.Serve(); defer srv.Shutdown()
 base := "http://"+srv.Addr()
 req, err := http.NewRequest("POST",base+"/api/v1/keys/"+platform.Key.ID+"/rotate",nil); must(err)
 req.Header.Set("Authorization","Bearer "+admin.Plaintext)
 resp, err := http.DefaultClient.Do(req); must(err)
 var rotated tenancy.NewAPIKeyResult; must(json.NewDecoder(resp.Body).Decode(&rotated)); resp.Body.Close()
 p, err := store.Resolve(ctx,rotated.Plaintext); must(err)
 fmt.Printf("SR-01: admin rotates platform key: HTTP=%d, new principal role=%s\n",resp.StatusCode,p.Role)
 if resp.StatusCode != 200 || p.Role != tenancy.RolePlatform { panic("SR-01 no longer reproduces") }
 req, err = http.NewRequest("GET",base+"/metrics",nil); must(err); req.Header.Set("Authorization","Bearer "+viewer.Plaintext)
 resp,err=http.DefaultClient.Do(req);must(err); b,err:=io.ReadAll(resp.Body);must(err);resp.Body.Close()
 exposed:=strings.Contains(string(b),"other-tenant-confidential-agent")
 fmt.Printf("SR-02: viewer metrics: HTTP=%d, other tenant metric exposed=%v\n",resp.StatusCode,exposed)
 if !exposed || resp.StatusCode!=200 {panic("SR-02 no longer reproduces")}
 replica, err := tenancy.NewSQLiteStore(filepath.Join(dir,"tenancy.db")); must(err); defer replica.Close()
 replica.EnableAuthCache(5*time.Minute,100)
 _,err=replica.Resolve(ctx,admin.Plaintext);must(err)
 must(store.DeleteAPIKey(ctx,tenant.ID,admin.Key.ID))
 cached,cacheErr:=replica.Resolve(ctx,admin.Plaintext)
 _,dbErr:=store.Resolve(ctx,admin.Plaintext)
 fmt.Printf("SR-03: deleted key accepted by other cached store=%v, authoritative lookup rejected=%v\n",cacheErr==nil&&cached!=nil,dbErr!=nil)
 if cacheErr!=nil || dbErr==nil {panic("SR-03 no longer reproduces")}
 secret:="ghp_"+strings.Repeat("A",36)
 redactor,err:=recording.NewRedactor(nil);must(err)
 it:=&recording.Interaction{Streaming:true,StreamEvents:[]recording.StreamEvent{{Data:"data: {\"key\":\""+secret},{Data:"\"}\n\n"}}}
 redactor.Apply(it)
 leaked:=strings.Contains(it.StreamEvents[0].Data,secret)
 fmt.Printf("SR-04: complete GitHub token inside split JSON SSE frame survives redaction=%v\n",leaked)
 if !leaked {panic("SR-04 no longer reproduces")}
}
