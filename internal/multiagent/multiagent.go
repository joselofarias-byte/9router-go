package multiagent

import (
 "context"
 "encoding/json"
 "errors"
 "net/http"
 "net/http/httptest"
 "strings"
 "sync"
 "time"
)

type Request struct {
 Prompt string `json:"prompt"`
 Models []string `json:"models"`
 Concurrency int `json:"concurrency"`
 TimeoutMS int `json:"timeout_ms"`
 AllowExternal bool `json:"allow_external"`
}
type Result struct {
 Model string `json:"model"`
 Output string `json:"output,omitempty"`
 Error string `json:"error,omitempty"`
 DurationMS int64 `json:"duration_ms"`
}
type Response struct { Results []Result `json:"results"` }

type Invoker func(context.Context,string,string)(string,error)

// Run isolates each model invocation, preserves request order and respects cancellation.
func Run(ctx context.Context, prompt string, models []string, concurrency int, timeout time.Duration, invoke Invoker) []Result {
 results:=make([]Result,len(models))
 sem:=make(chan struct{},concurrency)
 var wg sync.WaitGroup
 for i,model:=range models {
  wg.Add(1)
  go func(i int, model string) {
   defer wg.Done()
   results[i].Model=model
   start:=time.Now()
   select {
   case sem<-struct{}{}:
    defer func(){<-sem}()
   case <-ctx.Done():
    results[i].Error="cancelled"
    return
   }
   taskCtx,cancel:=context.WithTimeout(ctx,timeout)
   defer cancel()
   output,err:=invoke(taskCtx,model,prompt)
   results[i].DurationMS=time.Since(start).Milliseconds()
   if err!=nil { results[i].Error="model request failed"; return }
   if taskCtx.Err()!=nil {results[i].Error="cancelled or timed out"; return}
   results[i].Output=output
  }(i,model)
 }
 wg.Wait()
 return results
}

// HostInvoker calls the existing chat handler, keeping provider credentials in the host.
func HostInvoker(handler http.HandlerFunc) Invoker {
 return func(ctx context.Context,model,prompt string)(string,error) {
  payload,err:=json.Marshal(map[string]any{"model":model,"stream":false,"messages":[]map[string]string{{"role":"user","content":prompt}}})
  if err!=nil{return "",errors.New("invalid request")}
  req:=httptest.NewRequest(http.MethodPost,"/v1/chat/completions",strings.NewReader(string(payload))).WithContext(ctx)
  req.Header.Set("Content-Type","application/json")
  rec:=httptest.NewRecorder()
  handler(rec,req)
  if rec.Code!=http.StatusOK {return "",errors.New("upstream failed")}
  var response struct { Choices []struct {Message struct {Content string `json:"content"`} `json:"message"`} `json:"choices"` }
  if json.Unmarshal(rec.Body.Bytes(),&response)!=nil || len(response.Choices)==0 {return "",errors.New("invalid upstream response")}
  return response.Choices[0].Message.Content,nil
 }
}

// Handler MUST be registered only inside the existing dashboard-authenticated router group.
func Handler(invoke Invoker) http.HandlerFunc {
 return func(w http.ResponseWriter,r *http.Request) {
  w.Header().Set("Content-Type","application/json")
  r.Body=http.MaxBytesReader(w,r.Body,1<<20)
  var req Request
  dec:=json.NewDecoder(r.Body)
  dec.DisallowUnknownFields()
  if dec.Decode(&req)!=nil {http.Error(w,"invalid JSON",400);return}
  if !req.AllowExternal {http.Error(w,"explicit external dispatch authorization required",403);return}
  if strings.TrimSpace(req.Prompt)=="" || len(req.Models)==0 || len(req.Models)>8 || req.Concurrency<0 || req.Concurrency>8 || req.TimeoutMS<0 || req.TimeoutMS>120000 {http.Error(w,"invalid request",400);return}
  for _,model:=range req.Models {if strings.TrimSpace(model)=="" {http.Error(w,"invalid model",400);return}}
  concurrency:=req.Concurrency
  if concurrency==0 {concurrency=3}
  timeout:=time.Duration(req.TimeoutMS)*time.Millisecond
  if timeout==0 {timeout=60*time.Second}
  _=json.NewEncoder(w).Encode(Response{Results:Run(r.Context(),req.Prompt,req.Models,concurrency,timeout,invoke)})
 }
}
