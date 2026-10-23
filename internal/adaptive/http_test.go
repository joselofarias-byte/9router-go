package adaptive

import (
 "net/http"
 "net/http/httptest"
 "strings"
 "testing"
)

func TestExportHTTPFailClosed(t *testing.T) {
 for _,fn:=range []Authorized{nil,func(*http.Request)bool{return false}} {
  req:=httptest.NewRequest(http.MethodGet,"/admin/shadow",nil)
  w:=httptest.NewRecorder()
  NewExportHandler(fn).ServeHTTP(w,req)
  if w.Code!=http.StatusForbidden {t.Fatalf("expected 403, got %d",w.Code)}
  if strings.Contains(w.Body.String(),"routes") {t.Fatal("leaked export")}
  if w.Header().Get("Cache-Control")!="no-store" {t.Fatal("cache control missing")}
 }
}

func TestExportHTTPAuthenticatedAndReadOnly(t *testing.T) {
 handler:=NewExportHandler(func(*http.Request)bool{return true})
 post:=httptest.NewRecorder()
 handler.ServeHTTP(post,httptest.NewRequest(http.MethodPost,"/admin/shadow",nil))
 if post.Code!=http.StatusMethodNotAllowed {t.Fatalf("POST expected 405, got %d",post.Code)}
 get:=httptest.NewRecorder()
 handler.ServeHTTP(get,httptest.NewRequest(http.MethodGet,"/admin/shadow",nil))
 if get.Code!=http.StatusOK||!strings.Contains(get.Body.String(),`"schema_version":1`) {t.Fatalf("invalid export: %d %s",get.Code,get.Body.String())}
 if get.Header().Get("X-Content-Type-Options")!="nosniff" {t.Fatal("missing nosniff")}
}
