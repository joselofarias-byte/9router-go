package multiagent

import (
 "context"
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
