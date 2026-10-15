# Magnitude: puntos reales de integracion (revision de codigo)

Revision: 2026-10-15. Rama experimental.

## Evidencia directa
- `internal/providers/providers.go`: `ProviderConfig` soporta `BaseURL`, `NoAuth`, `DefaultAPIKey` y `LocalOnly`.
- `KnownProviders` incluye `llamacpp` en `http://127.0.0.1:8080/v1/chat/completions` y `ollama-local` en `http://127.0.0.1:11434/v1/chat/completions`, ambos con `LocalOnly: true`.
- `internal/handlers/chat/chat.go`: `HandleChatCompletions` resuelve el modelo y pasa por `handleSingleModel` / `handleAccountFallback`; el camino Claude traduce a OpenAI cuando corresponde.
- `docs/ECOSYSTEM_STRATEGY.md`: principio de integrar antes de construir.

## Decision
No agregar todavia un ID `magnitude` a `KnownProviders`: falta confirmar puerto real, soporte de endpoint y mecanismo de registro de modelos/conexiones. El puerto 8080 del script de smoke es un valor ilustrativo, no un puerto verificado de Magnitude.

## Siguiente trabajo verificable
1. Rastrear registro de conexiones, mapeo de IDs de proveedor y catalogo de modelos para `llamacpp` y `ollama-local`.
2. Comprobar si un `BaseURL` configurable permite una prueba sin cambiar Go.
3. Revisar `LocalOnly` y controles contra SSRF/redireccion antes de admitir direcciones remotas.
4. Probar `/v1/models`, chat normal, SSE y tool calls con una instancia real.
5. Solo entonces agregar proveedor opcional, tests y documentacion.

## Limites
No se ha ejecutado Magnitude, no se ha hecho benchmark y no se han cambiado ejecutores. Ninguna ganancia de rendimiento esta confirmada en infraestructura JolufaSoft.
