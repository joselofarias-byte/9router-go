package adaptive

import (
 "net/http"
)

// Authorized is provided by the existing administrative authentication layer.
// This handler deliberately does not implement credentials, sessions or tokens.
type Authorized func(*http.Request) bool

// NewExportHandler creates an unmounted read-only HTTP handler. Callers MUST
// provide an authorization check and mount it only behind protected admin routes.
// The handler is intentionally not wired into production.
func NewExportHandler(authorized Authorized) http.Handler {
 return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
  w.Header().Set("Cache-Control", "no-store")
  w.Header().Set("X-Content-Type-Options", "nosniff")
  if authorized==nil || !authorized(r) {
   http.Error(w,"forbidden",http.StatusForbidden)
   return
  }
  if r.Method!=http.MethodGet {
   w.Header().Set("Allow", "GET")
   http.Error(w,"method not allowed",http.StatusMethodNotAllowed)
   return
  }
  data,err:=ShadowExportJSON()
  if err!=nil {http.Error(w,"export unavailable",http.StatusInternalServerError);return}
  w.Header().Set("Content-Type","application/json; charset=utf-8")
  _,_=w.Write(data)
 })
}
