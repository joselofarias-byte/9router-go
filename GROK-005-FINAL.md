# GROK-005 — Informe final del motor Free Best

Fecha de verificación: 2026-10-21. Rama: `cursor/free-best-engine-f768`. Commit de la implementación: `a6a95cf0ff92704e00ee0d62344bc47d05568242`. No se fusionó a `main` y no hubo despliegue.

## Base elegida

La base de desarrollo es **`main` en `e3a2de8b9137474b13509f975e7f0ecfd5688ce6`**. Ese es el `HEAD` que GitHub tenía al empezar, y coincide con la referencia del brief (`feat(android): Capimux APK autónomo…`).

`feat/free-best-dynamic-routing` (`6a8b2d9`) se separa de `main` en `42ad2b8`. Tiene 13 commits que `main` no contiene, pero `main` tiene decenas de commits posteriores que esa rama no tiene, entre ellos el failover de cuentas ante HTTP 500 y errores de transporte (`0f54ded`, #124), el rechazo de respuestas vacías (`0421c6f`) y el APK. Reconstruir el motor encima de la rama experimental habría perdido ese trabajo. Los cambios útiles de esa línea (pool dinámico, cuentas reales, llama.cpp sin fila de credencial, fail-closed) ya están en `main` con otra historia.

Los PR de seguridad abiertos no se incorporaron. Siguen abiertos, en borrador y con base `main`:

| PR | Rama | Alcance | Relación con Free Best |
| --- | --- | --- | --- |
| #139 | `security/grok-004-data01-privacy` | Diagnóstico de requests solo para admin; sin persistir prompt por defecto | No cambia el enrutamiento. El diagnóstico nuevo sigue el mismo criterio: admin o token CLI, sin secretos. |
| #140 | `security/grok-004-ops01-updater` | Checksums y manifiesto Ed25519 del updater | Fuera del motor. |
| #141 | `security/grok-004-db02-encryption-design` | Diseño de cifrado de campos, sin migrar SQLite | Fuera del motor. No se tocó ninguna base de producción. |

## Inventario verificado en el código

| Pieza | Dónde | Qué hace hoy |
| --- | --- | --- |
| Discovery / Fabric | `internal/controlplane/discovery/` | Adapta catálogos (builtin free, llama.cpp, Cline, OrcaRouter, UnoRouter, Experiential, models.dev). `PricingMode` queda en `free`, `free_tier`, `paid` o `unknown`. |
| Registro | `internal/controlplane/registry/` | Snapshot en memoria. Las cuentas no llevan `AuthData`. |
| Política free-only | `internal/controlplane/routing/policy.go` | `PolicyFreeOnly` acepta solo `free` y `free_tier`. Vacío, `FREE`, `trial`, `paid` y `unknown` no entran. |
| Free Best | `internal/handlers/chat/resolution.go` | `free`, `free-best`, `fast-free`, `reasoning-free`, `coding-best-free`, `coding-auto`, `long-context-free`. Si no hay ruta elegible, `503` `free_route_unavailable`. No reenvía el nombre a un proveedor pago. |
| Cuentas | `internal/handlers/chat/fallback.go` | Rota cuentas de un mismo proveedor. |
| Proveedores | `internal/handlers/chat/combo.go` | Recorre el pool. `AllowVirtualFreeHop` vuelve a comprobar precio y cuenta antes de cada salto. |
| llama.cpp | `internal/controlplane/discovery/llamacpp.go`, `internal/handlers/chat/free_route_llamacpp_test.go` | Proveedor `NoAuth`. Entra al pool gratis sin inventar una fila de credencial y sin mandar `Authorization`. |
| Streaming | `usable_response.go`, `response_writer.go` | Una respuesta vacía no se confirma. Si ya salieron bytes, no hay otro intento. |
| Confianza | `internal/controlplane/trust/state.go` | Cuarentena local por auth o fallos repetidos. No consulta la cuota remota. |

## Qué se cambió

1. **Anuncio.** `GET /v1/models` solo añade los nombres virtuales cuando `resolveModel` encuentra un pool gratis usable. Sin ruta, no aparecen.
2. **Política única de fallo** (`failure_policy.go`), usada por el fallback de cuentas y por los dos bucles de combo (OpenAI y Anthropic).
3. **Anthropic.** `handleMessagesComboFallback` ahora se detiene si la respuesta ya empezó y trata el completion vacío como fallo del modelo, igual que el camino OpenAI.
4. **400/404.** No se gastan las otras cuentas del mismo modelo. Se pasa al siguiente proveedor elegible.
5. **402.** Se trata como cuota de esa cuenta: cooldown y otra cuenta. No abre una ruta de costo desconocido.
6. **500, 408, transporte y timeout de un salto.** Otra cuenta o proveedor, sin marcar la cuota como agotada. El timeout del caller sigue abortando.
7. **Diagnóstico.** `GET /api/admin/free-best?model=free-best` (también `GET /api/admin/explain-route`). Solo sesión admin o token CLI local. La respuesta lleva orden, precio, disponibilidad (`verified` / `unknown`), cuota (`unknown`, `known_exhausted`, `auth_failure`) y descarte (`paid`, `unknown_cost`, `inactive_*`, `no_account`, `cooldown`, `auth_failure`). No incluye API keys, tokens, cookies ni el texto del error upstream.

## Matriz de fallos

| Condición | Cuenta siguiente | Proveedor siguiente | Lock de cuota | Respuesta al cliente |
| --- | --- | --- | --- | --- |
| 401 | sí | si no quedan cuentas | sí, auth | la primera que responda de verdad |
| 403 | sí | si no quedan cuentas | sí | igual |
| 402 | sí | si no quedan cuentas | sí, cuota | igual |
| 429 | sí | si no quedan cuentas | sí, rate limit, respeta Retry-After | igual |
| 500 | sí | si no quedan cuentas | no | igual |
| 503 / 502 / 504 | sí | si no quedan cuentas | sí, gateway | igual |
| 408, timeout de un salto, red | sí | si no quedan cuentas | no | igual |
| 400 / 404 y otros 4xx no listados | no | sí, si hay otro elegible | no | error si nadie responde |
| cuerpo 200 vacío | no en el mismo modelo del combo | sí | no (lock corto solo de ese modelo) | no se entrega el vacío |
| bytes ya enviados al cliente | no | no | no | se conserva lo ya enviado |
| cancelación del caller | no | no | no | se aborta |
| `paid` o costo `unknown` | no aplica | nunca en el pool virtual | no aplica | `503 free_route_unavailable` si no queda nadie gratis |
| llama.cpp `NoAuth` con modelo `free` | ruta sintética `noauth:` | sí, como otro hop gratis | según el fallo | sin header `Authorization` |

Un 403 cuyo `Content-Type` el servidor detecta como `text/plain` sigue teniendo un reintento directo dentro de `proxy.DoRequest` (`isProxyFailure`). Eso ya estaba. No es una rotación de cuenta ni una vía paga. La prueba de failover de 403 fija `application/json` para medir la rotación de cuenta.

## Comprobado

Ejecutado con Go **1.27.0** (`/tmp/go1.27/go/bin/go`, `GOTOOLCHAIN=local`). El `go` del sistema es 1.22 y no puede compilar este módulo.

- `go build ./...` — exit 0. Para compilar hizo falta un `web/dist/index.html` local porque `web/embed.go` embebe `dist/*` y el directorio está en `.gitignore`. Ese archivo no forma parte del commit.
- `go test -count=1 ./internal/handlers/chat/ ./internal/providers/ ./internal/controlplane/... ./internal/proxy/...` — todos `ok`. Incluye streaming, OpenAI, Anthropic/messages, llama.cpp, respuestas vacías y los tests nuevos.
- `go test -count=1 ./...` — el resto de paquetes `ok`, con dos excepciones de assets estáticos (abajo).
- `go test -tags=integration -count=1 -run 'TestFreeBestProductionRouterFailover|TestChatCompletion|TestOpencodeEmpty|TestStreaming|TestMessages' ./internal/integration/` — `ok`.

Pruebas nuevas que cubren el criterio de éxito:

- `TestFreeBestFailoverMatrix`: 401, 403, 402, 429, 500, 503 y conexión rechazada. El segundo proveedor gratis responde. El upstream pago (`openai/gpt-4o`) recibe 0 requests. El modelo que llega al proveedor sano es el modelo gratis, no el pago.
- `TestFreeBestPermanentRequestDoesNotBurnSiblingAccount`: un 400 no llama a la otra cuenta Groq y sí al siguiente proveedor gratis.
- `TestFreeBestAuthRotatesSiblingAccount`: un 401 pasa a la otra cuenta y deja `rateLimitedUntil` en la primera.
- `TestAccountFallbackQuota402RotatesAnd500DoesNotLock`: 402 escribe cooldown; 500 no.
- `TestMessagesComboDoesNotReplayPartialStream`: el stream Anthropic que ya emitió `parcial` no llama al segundo upstream.
- `TestFreeBestPerHopTimeoutUsesNextProvider`: timeout de 200 ms del cliente y el siguiente proveedor responde. El lento se llamó una sola vez.
- `TestExplainFreeRouteOmitsSecrets` y el test de integración: el JSON de diagnóstico no contiene las API keys ni el texto `Bearer` del `lastError`.
- `TestHandleModels_ListsVirtualFreeRoutes`: sin pool, los nombres virtuales no salen. `TestHandleModels_AdvertisesVirtualRoutesOnlyWhenRoutable`: con pool, sí.
- `TestFreeBestProductionRouterFailover`: router de producción (`app.ProvideRouter`), SQLite temporal, tres upstreams locales. `free-best` sobrevive a un 503, no llama al pago, lista el nombre y niega el diagnóstico a la API key de inferencia (401). El token CLI local sí lo lee.

No se usaron credenciales reales, ni APIs de pago, ni bases de Oracle, Termux o producción.

## Pendiente

- No se fusionó `feat/free-best-dynamic-routing` ni los PR #139, #140 y #141. Siguen abiertos para su propia revisión.
- La cuota numérica que publican los proveedores (requests restantes, ventana de 5 h) no se consulta en vivo. `known_exhausted` significa cooldown local ya registrado. Si no hay cooldown, la cuota figura como `unknown` aunque el modelo sea elegible.
- El estimador de usage puede seguir anotando un costo de catálogo distinto de cero en el log de un modelo marcado `free_tier` (se vio `cost≈8e-06` en un salto de prueba local). Eso no dispara una llamada paga. No se cambió la tabla de precios.
- `go test ./...` falló en `internal/handlers` (`TestSetupServerRouter_SPARoutes`) y `web` (`TestHandler_PWAAssets`): piden `sw.js`, manifest e iconos que no están en el `web/dist` de este entorno. No es una regresión del motor. No se construyó el SPA (no hay `bun` en la máquina; el build de Vite quedó fuera).
- No se ejecutó la suite `integration` completa, ni `go test -race`, ni pruebas con un binario OpenCode real, ni Android/Termux.
- El segundo pase del combo, cuando nadie respondió y hay un `Retry-After` acotado, sigue existiendo. No corre si el cliente ya recibió bytes.
- Un 403 con cuerpo olido como `text/plain` todavía hace un reintento de transporte directo antes de rotar la cuenta.
