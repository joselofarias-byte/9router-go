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

## Ronda 2

Fecha: 2026-10-21. Commit de la implementación: `9d585e411834877412de8020258bcb2d4fde1cef`. Misma rama `cursor/free-best-engine-f768`, mismo PR #142. No se fusionó y no hubo despliegue. Los PR #139, #140 y #141 no se incorporaron.

### Diagnóstico

`ExplainFreeRoute` ya no elige “la mejor” cuenta del proveedor ni escribe `pricing: "free"` a mano.

- `pricing` es el `PricingMode` del registro (`free` o `free_tier`, o el valor real si la fila no es gratis). Si no hay fila, el campo queda vacío.
- Cada `providerConnections` sale en `accounts[]` con su propio `accountId`, `state`, `quota` y `quotaKnown`.
- `quota_exhausted` es solo HTTP 402. `rate_limited` es solo HTTP 429. Los dos ponen `quotaKnown: true` y no inventan un saldo numérico.
- 401/403 es `auth_failure`. 502/503/504 es `gateway_temporary`. En ambos `quota` sigue siendo `unknown` y `quotaKnown` es false.
- Un cooldown cuyo `lastError.status` no es uno de esos queda `cooldown_unclassified`, también con cuota `unknown`. No se llama cuota agotada.
- Sin cooldown, `state` es `eligible` y `quota` es `unknown`. No aparece `0`, `"0"` ni `"available"`.
- `availability: "recent_success"` significa que hubo un turno reciente con éxito. No es capacidad verificada. Sin esa evidencia, `availability` es `unknown`.
- Una cuenta inactiva no hereda el estado de una hermana. El descarte de una fila gratis no elegida repite el estado de cada cuenta bloqueada.
- llama.cpp `NoAuth` sin fila en la base aparece como la cuenta sintética `noauth:<provider>`, cuota `unknown`.
- El JSON sigue sin API key, token, cookie, email ni texto de `lastError`.

### 403 text/plain y cancelación

`proxy.DoRequest` y `FallbackTransport` reenvían por el transporte directo solo si esa petición iba por un proxy y el 403 es un rechazo de proxy (`X-Proxy-Error`, o cuerpo `text/plain` que nombra proxy, túnel o allowlist). Un 403 `text/plain` de aplicación (`invalid api key`) es un solo salto. El mismo criterio quedó en el cliente de Freebuff. Un `context` ya cancelado corta el combo de OpenAI y el de Anthropic antes de marcar el siguiente proveedor.

### Comandos y resultados

Go 1.27.0, `GOTOOLCHAIN=local`, binario `/tmp/go1.27/go/bin/go`. `go test` reescribió `go.mod`; se revirtió con `git checkout -- go.mod` y no entra en el commit.

| Comando | Resultado |
| --- | --- |
| `go test -count=1 -timeout 180s ./internal/handlers/chat/ ./internal/proxy/ ./internal/proxy/executor/` (filtro de los tests nuevos y de failover) | `ok` chat 0.215s, proxy 0.008s. El paquete executor compiló; el filtro no seleccionó tests suyos. |
| `go test -count=1 -timeout 180s ./internal/handlers/chat/ ./internal/proxy/...` | `ok`. chat 8.437s, proxy 0.231s, executor 0.028s, oauth 0.006s. |
| `go test -tags=integration -count=1 -timeout 180s -run 'TestFreeBestProductionRouterFailover\|TestChatCompletion\|TestOpencodeEmpty\|TestStreaming\|TestMessages' ./internal/integration/` | `ok` 0.109s. Upstreams locales. El diagnóstico de ese router muestra `free_tier`, `gateway_temporary` tras el 503 y no un saldo cero. |
| `go test -race -count=1 -timeout 20m ./internal/handlers/chat/ ./internal/handlers/ ./internal/controlplane/... ./internal/proxy/... ./internal/providers/ ./internal/db/` | Ver el párrafo siguiente. No hubo línea `DATA RACE`. |
| `go test -race -count=3 -timeout 120s -run 'TestHandleMessagesComboFallback_RetriesOnceOnBoundedRetryAfter$' ./internal/handlers/chat/` | `ok` 3.666s. |
| `go test -race -count=1 -timeout 15m ./internal/handlers/chat/` | `ok` 23.364s. |
| `go build ./...` | exit 0. |
| `bun install --frozen-lockfile && bun test && bun run build` en `web/` (Bun 1.4.3, instalado en esta máquina) | 117 pass, 0 fail. El build de Vite terminó. Avisos de accesibilidad de Svelte ya existentes; no se tocaron. `web/dist` no se commitea. |
| `go test -count=1 -run 'TestSetupServerRouter_SPARoutes\|TestHandler_PWAAssets' ./internal/handlers/ ./web/` después de ese build | `ok`. |

En la pasada `-race` que lanzó varios paquetes a la vez, `TestHandleMessagesComboFallback_RetriesOnceOnBoundedRetryAfter` falló una vez: esperaba 2 golpes al upstream y vio 1, en 0.04s. El log no contiene `DATA RACE`. La misma prueba, aislada con `-race -count=3`, y después el paquete `chat` entero con `-race`, pasaron. El segundo pase del combo sigue dependiendo de un `Retry-After` de ~1s que quepa en la tolerancia de 2s y de que el lock de la cuenta ya haya vencido. Bajo carga ese margen se pierde y no hay reintento. No es un bucle. No reescribí el contrato de esa prueba.

`internal/handlers` falló en esa misma pasada `-race` solo en `TestSetupServerRouter_SPARoutes` (404 de `sw.js`, manifest e icono) porque `web/dist` era un stub. Tras `bun run build` el mismo test pasa. CI construye el SPA antes de `go test`, así que ese fallo es de este entorno y no del motor.

`go test -race` de controlplane (discovery, registry, routing, scoring, sync, trust, verification), proxy, executor, oauth, providers y db terminó `ok` en esa pasada.

CI del commit `8568e0088378b1518b9106cc097124ee248f2bdc`, comprobada con `gh pr checks 142` y `gh run view` el 2026-10-21: los seis checks terminaron en éxito. Runs `39669531078` (CI) y `39669531052` (Capimux Multiagent PR Validation), ambos `conclusion: success` y `headSha` igual a ese commit. Jobs: `test` (2m11s), `Integration tests` (1m10s), `Build Docker image (verify only)` (1m29s), `multiagent` (1m38s), más los checks de preview de Vercel. Esa pasada de integración es la del workflow, con `-race`, no una repetición local. No se fusionó y no se desplegó a producción; el check de Vercel es el preview que el PR dispara solo.

### Protocolo Termux / HONOR 200

No se ejecutó. No se instaló nada en un teléfono, no se abrió Termux y no se apuntó el binario a un proveedor real. CI ya compila `GOOS=android GOARCH=arm64` en el job `test` (`go build -o /tmp/9router-go-termux-arm64`), pero eso no es una prueba en el dispositivo.

Para quien tenga el HONOR 200:

1. Instalar Termux desde F-Droid. No usar el binario de la tienda de Play: es otro entorno.
2. Dentro de Termux: `pkg install golang git` solo si se va a compilar ahí. Si se usa el artefacto de CI, basta `pkg install curl` y copiar el binario arm64. No hace falta una cuenta de proveedor.
3. Crear un directorio vacío, por ejemplo `~/capimux-r2`, y exportar `DATA_DIR` a ese directorio. No reutilizar `~/.9router` ni ninguna base que ya tenga claves.
4. Arrancar con un solo upstream local. En otro puerto de la misma máquina, un proceso compatible con llama.cpp (o `llama-server`) en `127.0.0.1`. Registrar ese proveedor como `llamacpp`, precio `free`, sin API key.
5. Pedir `free-best` por `POST /v1/chat/completions` contra el gateway local. Comprobar que la petición llega a llama.cpp y que no sale `Authorization`.
6. Apagar llama.cpp y repetir. La respuesta debe ser `503` `free_route_unavailable`. No debe aparecer un modelo de pago.
7. `GET /api/admin/free-best` con el token CLI local. El JSON no debe contener la clave ni el cuerpo de error. `quota` de la ruta local debe ser `unknown`, no `0`.
8. Cortar el cliente a mitad de un stream (Ctrl-C del `curl`). No debe haber un segundo `POST` a llama.cpp.
9. Borrar `DATA_DIR` al terminar.

Si alguno de esos pasos llama a Groq, OpenAI, DeepSeek u otro host que no sea loopback, la prueba deja de ser válida para este informe.

### Qué falta para aprobar la fusión

1. Revisión humana del diff de #142. Sigue en borrador. Este informe no la sustituye.
2. Decisión explícita de dejar #139, #140 y #141 fuera. No están en esta rama y no se probaron junto con este motor.
3. La cuota numérica remota (requests restantes, ventana de 5 h) no se lee. `quotaKnown: true` solo significa que un 402 o un 429 local dejó cooldown. Un saldo real desconocido sigue en `unknown`.
4. El protocolo Termux/HONOR 200 de arriba no se corrió.
5. La suite `integration` completa con `-race` pasó en el job `Integration tests` del commit `8568e00` (run `39669531078`). En local, sobre este commit, solo se corrió el filtro sin `-race`.
6. No se corrió `go test -race ./...` de todo el módulo. Los paquetes listados arriba sí, y el job `test` de esa CI corrió `go test ./...` sin `-race` después de construir el SPA.
7. El segundo pase del combo, con `Retry-After` dentro de 2s y sin bytes entregados, sigue existiendo. La prueba de 1s falló una vez bajo carga y luego pasó. No hay evidencia de bucle ilimitado: 12 cuentas con 401 produjeron como máximo 10 intentos, uno por cuenta.
8. El estimador de usage puede anotar un costo de catálogo distinto de cero en un modelo `free_tier`. No se cambió la tabla de precios y no se hizo ninguna llamada de pago.
9. Un 403 de proxy con cuerpo que nombra allowlist o túnel todavía se reintenta una vez por el transporte directo. Es el caso que el transporte directo existe para cubrir. Un 403 de aplicación no.

## Ronda 3

Fecha: 2026-10-22. Misma rama `cursor/free-best-engine-f768`, mismo PR #142 en borrador. No se fusionó, no hubo despliegue, no se tocó Oracle ni una base real, y no se usaron credenciales ni APIs de pago. Los PR #139, #140 y #141 siguen fuera.

La referencia revisada al empezar era `a5bbb3313ddb12c22d684b53d3cb19b01622a5ae`. El código de esta ronda es `e0bf4a30b2a34f80ef7b53b4e74a130d5d8fcaf5`. El head que CI ejecutó es `3fa9fa49407addf97172e226ce714a6944332d6e` (el commit anterior más esta línea de hash). La CI verde de la ronda 2 (`8568e00`) no cubre estos cambios.

### Cierre del cuerpo HTTP

`proxyRefusalBody` y `freebuffProxyRefusalBody` leían hasta 512 bytes y reconstruían el cuerpo con `io.NopCloser(io.MultiReader(...))`. `Close` de ese wrapper no hace nada, así que el cuerpo original quedaba abierto.

`TestProxyRefusalPeekClosesOriginalBody` se escribió contra ese código y registró `original Close calls=0` tanto en el 403 corto de aplicación (`invalid api key`) como en el rechazo largo (`blocked-by-allowlist` más 4000 bytes). Esa corrida es anterior al arreglo.

El arreglo es `PrefixReadCloser`: vuelve a poner delante los bytes ya leídos y, al cerrar, cierra el cuerpo original. `Read` no trata el fin del prefijo como fin del cuerpo, porque si no `io.ReadAll` se quedaba con los primeros 512 bytes. Los dos sitios de peek usan ese tipo. El contenido que el caller lee después es el mismo que llegó.

`DoRequest` y `DoFreebuffHTTP` no añaden otro salto directo cuando el transporte del cliente ya es `FallbackTransport` (también si hay un wrapper cuyo `Unwrap` llega a ese tipo). `FallbackTransport` sigue haciendo su único reintento directo. Un 403 de aplicación no se reintenta en ninguna de las dos capas.

### Diagnóstico y bloqueo por modelo

La hipótesis era cierta en el código, y el test la fija. El combo y `getBestConnection` saltan una cuenta con `IsConnectionModelLocked` (`modelLock_<modelo>`). `classifyAccount` solo miraba `rateLimitedUntil`. Una cuenta con un modelo bloqueado y otro libre salía `eligible` en todos los hops.

`ExplainFreeRoute` ahora clasifica cada hop con el modelo de esa ruta. Si el lock de ese modelo está en el futuro y la cuenta no tiene otro cooldown, el estado es `model_locked`, `quota` es `unknown` y `quotaKnown` es false. El otro modelo de la misma cuenta sigue `eligible` con la misma cuota desconocida. No se escribe un saldo.

`TestComboSkipsLockedModelAndCallsTheOpenOne` usa los dos modelos gratis de Groq en un solo upstream local. El modelo bloqueado recibe 0 requests. El abierto recibe 1 y la respuesta es 200.

### Retry-After

`TestHandleMessagesComboFallback_RetriesOnceOnBoundedRetryAfter` armaba un instante RFC3339 a un segundo. `Format(time.RFC3339)` tira la fracción, así que bajo carga la espera medida podía ser de unas decenas de milisegundos mientras el lock se redondeaba a 1s. El segundo pase no veía la cuenta y el test terminaba en ~0.04s con un solo golpe. No era una carrera de memoria.

El header de esa prueba ahora es `Retry-After: 1` (delta-seconds, `application/json`). Antes del segundo pase, el combo suelta solo los locks que ese mismo request escribió (`ClearConnectionRateLimit` y `UnlockConnectionModel`, incluida la clave canónica si difiere). Un lock de otro modelo de la misma cuenta se queda. La tolerancia de 2s no cambió: un 429 más largo no gana otro pase.

### Pruebas ejecutadas

Go 1.27.0, `GOTOOLCHAIN=local`, `/tmp/go1.27/go/bin/go`, directorio `/workspace`. `go test` volvió a reescribir `go.mod`; se revirtió con `git checkout -- go.mod` y no entra en el commit. No hubo línea `DATA RACE` en ninguna pasada `-race` de esta ronda.

| Comando | Resultado |
| --- | --- |
| `go test -count=1 -timeout 120s -run 'TestProxyRefusalPeekClosesOriginalBody\|TestDoRequestClosesPeekedBodies\|TestDoRequestDoesNotReplayAgainBehindFallbackTransport\|TestIsProxyFailure\|TestDoRequest' ./internal/proxy/` | `ok` 0.008s |
| `go test -count=1 -timeout 180s -run 'TestFreebuffRefusalPeekClosesOriginalBody\|TestDoFreebuffHTTPClosesPeekedBodies\|TestDoFreebuffHTTPDoesNotReplayAgainBehindFallbackTransport\|TestFreebuffCountry' ./internal/proxy/executor/` | `ok` 0.007s |
| `go test -count=1 -timeout 180s -run 'TestExplainFreeRoute\|TestClassifyAccount\|TestComboSkipsLockedModel\|TestReleaseRetriedLocks\|TestHandleMessagesComboFallback_RetriesOnceOnBoundedRetryAfter\|TestHandleMessagesComboFallback_Retries\|TestFreeBestSeveralAccounts\|TestFreeBest429\|TestFreeBestClientCancel\|TestComboPartialStream\|TestMessagesComboDoesNotReplay\|TestAccountFallbackDoesNotReplay\|TestFreeBestFailoverMatrix\|TestFreeBestProductionRouter' ./internal/handlers/chat/` | `ok` 1.145s. El filtro `TestFreeBestProductionRouter` no selecciona nada en este paquete; esa prueba está en `internal/integration` y se corrió aparte. |
| `go test -race -count=1 -timeout 240s ./internal/handlers/chat/ ./internal/proxy/ ./internal/db/` | `ok`. chat 23.317s, proxy 1.254s, db 2.819s. Incluye streaming, cancelación, la matriz free-only y el `Retry-After` de 1s. |
| `go test -race -count=5 -timeout 120s -run 'TestHandleMessagesComboFallback_RetriesOnceOnBoundedRetryAfter\|TestComboSkipsLockedModel\|TestExplainFreeRouteShowsModelLock\|TestReleaseRetriedLocks' ./internal/handlers/chat/` | `ok` 7.124s. Cinco repeticiones del retry de 1s, del lock de un modelo y de la liberación selectiva. |
| `go test -race -count=1 -timeout 180s -run 'TestFreebuffRefusalPeekClosesOriginalBody\|TestDoFreebuffHTTPClosesPeekedBodies\|TestDoFreebuffHTTPDoesNotReplayAgainBehindFallbackTransport\|TestFreebuffCountry\|TestRequestFreebuffSession' ./internal/proxy/executor/` | `ok` 1.041s |
| `go test -tags=integration -race -count=1 -timeout 180s -run 'TestFreeBestProductionRouterFailover' ./internal/integration/` | `ok` 1.187s. Router de producción, SQLite temporal, upstreams `httptest`. |
| `go build ./...` | exit 0 |

Cobertura que esos comandos sí ejercitaron:

- Cuerpo corto y largo, 403 de aplicación y rechazo de proxy, transporte directo y transporte con proxy, en `DoRequest` y en `DoFreebuffHTTP`. El cierre del cuerpo original queda en 1. Un rechazo de proxy seguido de 200 directo es un golpe al proxy y uno al directo. Detrás de `FallbackTransport`, el upstream real de esa URL recibe 0: no hay un tercer salto.
- Dos modelos gratis del mismo proveedor: el bloqueado no se llama; el abierto sí. El diagnóstico dice `model_locked` y `eligible`, ambos con `quota: unknown`.
- `paid` y `unknown` siguen en la matriz y en `TestFreeBestSeveralAccountsFailThenNextFreeProvider`, dentro del `go test -race` del paquete `chat`.
- `TestComboPartialStreamIsNotReplayed`, `TestMessagesComboDoesNotReplayPartialStream`, `TestAccountFallbackDoesNotReplayPartialStream` y `TestFreeBestClientCancelDoesNotWalkThePool` están en ese mismo paquete y la pasada `-race` del paquete terminó `ok`.

### CI de GitHub

Comprobada el 2026-10-22 con `gh pr checks 142` y `gh run view`, sobre el head `3fa9fa49407addf97172e226ce714a6944332d6e`. Los seis checks terminaron sin fallos. El PR seguía en borrador.

| Check | Resultado | Run |
| --- | --- | --- |
| `test` | pass, 2m10s | `39702649625` |
| `Integration tests` | pass, 45s | `39702649625` |
| `Build Docker image (verify only)` | pass, 1m24s | `39702649625` |
| `multiagent` | pass, 1m46s | `39702649794` |
| Vercel | pass | preview del PR |
| Vercel Preview Comments | pass | preview del PR |

`gh run view 39702649625` y `gh run view 39702649794` devuelven `conclusion: success`, `status: completed` y `headSha` igual a ese commit. En `.github/workflows/ci.yml` el job `test` corre `go test ./...` y el job `integration` corre `go test -tags=integration -race -count=1 -timeout 10m ./internal/integration/...`. No descargué el log de cada step. El check de Vercel es el preview que el PR dispara. No es un despliegue a producción.

El commit que escribe este párrafo arranca otra CI. Esa corrida no es el resultado de la tabla.

### Afirmaciones no verificadas

- La CI del commit que solo documenta la tabla de arriba. La tabla es de `3fa9fa4`, no de ese seguimiento.
- `go test -race ./...` del módulo entero. No se lanzó.
- La suite `integration` completa. En local solo se relanzó `TestFreeBestProductionRouterFailover` con `-race`.
- Dashboard (`bun test`, `bun run build`). Esta ronda no tocó `web/`.
- Protocolo Termux / HONOR 200. Sigue sin ejecutarse. El texto de la ronda 2 no cambió y no se corrió.
- Cuota numérica remota. No se consultó ningún proveedor. `model_locked` no es un saldo.
- Que un 429 de más de 2s siga sin segundo pase se apoya en las pruebas ya existentes del paquete `chat`, que pasaron en la pasada `-race` de esta ronda. No se rediseñó esa tolerancia y no se midió un proveedor real.
- El costo de catálogo distinto de cero en un modelo `free_tier` no se volvió a medir.
- Revisión humana del diff. Este informe no la sustituye.

### Aptitud para fusión

No está listo para fusionar. El PR sigue en borrador a propósito.

Lo que sí quedó demostrado en esta máquina, con los comandos de la tabla:

- el cuerpo original se cierra al cerrar el wrapper, y el contenido corto y largo se conserva;
- `FallbackTransport` no se combina con un segundo reintento de `DoRequest` ni de `DoFreebuffHTTP`;
- el diagnóstico y el selector coinciden cuando un modelo de la cuenta está bloqueado y el otro no;
- el `Retry-After` de 1s produce dos golpes, cinco veces bajo `-race`, en menos de 5s por pasada del paquete filtrado;
- el paquete `chat` entero, `proxy` y `db` pasan con `-race`;
- `paid` y `unknown` no reciben tráfico en las pruebas de failover que ese paquete ya contiene.

En GitHub, el head `3fa9fa4` tiene los seis checks en éxito, incluido `go test ./...` y la suite de integración con `-race`. Eso no sustituye la revisión humana.

Lo que impide aprobar la fusión, y que esta ronda no cierra:

1. Falta la revisión humana. #139, #140 y #141 no están en la rama y no se probaron juntos.
2. En local no se corrió `go test -race ./...` del módulo entero. El job `test` de `3fa9fa4` sí corrió `go test ./...` sin `-race`. El job `Integration tests` de ese mismo commit sí corrió la suite de integración con `-race`, según el workflow. En esta máquina solo se relanzó `TestFreeBestProductionRouterFailover`.
3. No hay lectura de cuota remota, ni corrida en Termux/HONOR 200, ni llamada a un proveedor real.
4. El segundo pase del combo, para un `Retry-After` de hasta 2s y sin bytes ya entregados, sigue siendo el comportamiento previsto. No es un bucle: el tope de cuentas y la tolerancia no se ampliaron.
