package multiagent

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync/atomic"
 "testing"
 "time"
)

func TestParallelAndOrder(t *testing.T) {
 var active,max atomic.Int32
 invoke:=func(ctx context.Context,m,p string)(string,error){
  n:=active.Add(1)
  for {v:=max.Load();if n<=v||max.CompareAndSwap(v,n){break}}
  defer active.Add(-1)
  time.Sleep(25*time.Millisecond)
  if m=="bad" {return "",errors.New("failure")}
  return m+":"+p,nil
 }
 results:=Run(context.Background(),"hello",[]string{"a","bad","c"},3,time.Second,invoke)
 if len(results)!=3 || results[0].Output!="a:hello" || results[1].Error=="" || results[2].Output!="c:hello" {t.Fatalf("unexpected results: %+v",results)}
 if max.Load()<2 {t.Fatalf("not parallel: %d",max.Load())}
}
func TestHTTPRejectsWithoutOptIn(t *testing.T){
 h:=Handler(func(context.Context,string,string)(string,error){t.Fatal("invoked without opt-in");return "",nil})
 w:=httptest.NewRecorder()
 r:=httptest.NewRequest("POST","/api/multiagent/run",strings.NewReader(`{"prompt":"hello","models":["a"]}`))
 h(w,r)
 if w.Code!=http.StatusForbidden {t.Fatalf("status %d",w.Code)}
}
func TestHostInvoker(t *testing.T){
 inv:=HostInvoker(func(w http.ResponseWriter,r *http.Request){w.Header().Set("Content-Type","application/json");w.Write([]byte(`{"choices":[{"message":{"content":"ok"}}]}`))})
 output,err:=inv(context.Background(),"test","hello")
 if err!=nil||output!="ok"{t.Fatalf("output %q err %v",output,err)}
}

func TestCancellationAndTimeout(t *testing.T) {
 invoke:=func(ctx context.Context,model,prompt string)(string,error) {
  <-ctx.Done()
  return "",ctx.Err()
 }
 start:=time.Now()
 results:=Run(context.Background(),"x",[]string{"slow"},1,20*time.Millisecond,invoke)
 if len(results)!=1 || results[0].Error=="" {t.Fatalf("expected timeout: %+v",results)}
 if time.Since(start)>time.Second {t.Fatal("timeout ignored")}
 ctx,cancel:=context.WithCancel(context.Background())
 cancel()
 results=Run(ctx,"x",[]string{"a","b"},1,time.Second,invoke)
 for _,r:=range results {if r.Error=="" {t.Fatalf("expected cancellation: %+v",results)}}
}
func TestHTTPRejectsTooManyModels(t *testing.T) {
 h:=Handler(func(context.Context,string,string)(string,error){t.Fatal("invoked invalid request");return "",nil})
 w:=httptest.NewRecorder()
 r:=httptest.NewRequest("POST","/api/multiagent/run",strings.NewReader(`{"prompt":"x","models":["1","2","3","4","5","6","7","8","9"],"allow_external":true}`))
 h(w,r)
 if w.Code!=http.StatusBadRequest {t.Fatalf("status %d",w.Code)}
}

func TestExternalDispatchDisabledByDefault(t *testing.T) {
 t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL","")
 h:=Handler(func(context.Context,string,string)(string,error){t.Fatal("invoked with external dispatch disabled");return "",nil})
 w:=httptest.NewRecorder()
 r:=httptest.NewRequest("POST","/api/multiagent/run",strings.NewReader(`{"prompt":"hello","models":["a"],"allow_external":true}`))
 h(w,r)
 if w.Code!=http.StatusForbidden {t.Fatalf("status %d",w.Code)}
}
func TestRejectTrailingJSON(t *testing.T) {
 t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL","1")
 h:=Handler(func(context.Context,string,string)(string,error){t.Fatal("invoked invalid request");return "",nil})
 w:=httptest.NewRecorder()
 r:=httptest.NewRequest("POST","/api/multiagent/run",strings.NewReader(`{"prompt":"x","models":["a"],"allow_external":true}{}`))
 h(w,r)
 if w.Code!=http.StatusBadRequest {t.Fatalf("status %d",w.Code)}
}

func TestModelAllowlistRequired(t *testing.T) {
 t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL","1")
 t.Setenv("CAPIMUX_MULTIAGENT_ALLOWED_MODELS","approved/model")
 invoked:=false
 h:=Handler(func(context.Context,string,string)(string,error){invoked=true;return "ok",nil})
 w:=httptest.NewRecorder()
 r:=httptest.NewRequest("POST","/api/multiagent/run",strings.NewReader(`{"prompt":"x","models":["unapproved/model"],"allow_external":true}`))
 h(w,r)
 if w.Code!=http.StatusForbidden || invoked {t.Fatalf("unexpected status %d invoked=%v",w.Code,invoked)}
 w=httptest.NewRecorder()
 r=httptest.NewRequest("POST","/api/multiagent/run",strings.NewReader(`{"prompt":"x","models":["approved/model"],"allow_external":true}`))
 h(w,r)
 if w.Code!=http.StatusOK || !invoked {t.Fatalf("approved model status %d invoked=%v",w.Code,invoked)}
}
func TestBatchCapacity(t *testing.T) {
 t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL","1")
 t.Setenv("CAPIMUX_MULTIAGENT_ALLOWED_MODELS","approved/model")
 batchSlots<-struct{}{}
 batchSlots<-struct{}{}
 defer func(){<-batchSlots;<-batchSlots}()
 h:=Handler(func(context.Context,string,string)(string,error){t.Fatal("invoked despite capacity");return "",nil})
 w:=httptest.NewRecorder()
 r:=httptest.NewRequest("POST","/api/multiagent/run",strings.NewReader(`{"prompt":"x","models":["approved/model"],"allow_external":true}`))
 h(w,r)
 if w.Code!=http.StatusTooManyRequests {t.Fatalf("status %d",w.Code)}
}

func TestRejectDuplicateModels(t *testing.T) {
 t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL","1")
 t.Setenv("CAPIMUX_MULTIAGENT_ALLOWED_MODELS","approved/model")
 h:=Handler(func(context.Context,string,string)(string,error){t.Fatal("invoked duplicate model");return "",nil})
 w:=httptest.NewRecorder()
 r:=httptest.NewRequest("POST","/api/multiagent/run",strings.NewReader(`{"prompt":"x","models":["approved/model","approved/model"],"allow_external":true}`))
 h(w,r)
 if w.Code!=http.StatusBadRequest {t.Fatalf("status %d",w.Code)}
}

func TestHTTPAuthorizedSuccessAndOrderedResults(t *testing.T) {
 t.Setenv("CAPIMUX_MULTIAGENT_ENABLE_EXTERNAL","1")
 t.Setenv("CAPIMUX_MULTIAGENT_ALLOWED_MODELS","model/a,model/b")
 h:=Handler(func(_ context.Context,m,p string)(string,error){return m+":"+p,nil})
 w:=httptest.NewRecorder()
 r:=httptest.NewRequest(http.MethodPost,"/api/multiagent/run",strings.NewReader(`{"prompt":"hello","models":["model/b","model/a"],"allow_external":true,"concurrency":2}`))
 h(w,r)
 if w.Code!=http.StatusOK {t.Fatalf("status %d body %s",w.Code,w.Body.String())}
 var response Response
 if err:=json.Unmarshal(w.Body.Bytes(),&response);err!=nil {t.Fatal(err)}
 if len(response.Results)!=2 || response.Results[0].Output!="model/b:hello" || response.Results[1].Output!="model/a:hello" {t.Fatalf("unexpected response %+v",response)}
}
