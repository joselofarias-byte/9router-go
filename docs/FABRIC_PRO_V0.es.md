# Fabric Pro v0 — plan de ejecución

Fecha: 2026-10-04

## Objetivo

Construir la primera porción de ingeniería capaz de justificar un plan Pro de USD 5–10/mes **sin vender inferencia ni tokens**.

La hipótesis a validar es que un usuario con dos o más recursos de IA pagará por reducir cambios manuales, aprovechar mejor sus cuotas y mantener continuidad cuando una cuenta o proveedor entra en cooldown, falla o queda sin capacidad.

## Contrato de producto

### Community debe seguir siendo útil

- gateway local;
- BYOK;
- modelos locales;
- conexiones a proveedores;
- selección manual;
- routing/fallback básico;
- métricas esenciales;
- configuración exportable;
- interoperabilidad con clientes externos.

### Pro vende orquestación

- decisiones multi-cuenta;
- awareness de cuota/reset/cooldown;
- routing por intención;
- explainability;
- presupuestos y políticas;
- telemetría avanzada de valor.

## Fase A — estado normalizado de capacidad

Crear o completar una representación común, independiente del proveedor, para cada cuenta/recurso:

- identidad de provider/account;
- estado: unknown, available, stale, exhausted;
- remaining y/o used cuando el proveedor lo exponga;
- reset_at;
- cooldown_until;
- observed_at;
- source;
- scope cuando varias credenciales compartan la misma cuota.

### Criterios de aceptación

- unknown nunca equivale a ilimitado;
- exhausted/cooldown no se selecciona cuando existe una alternativa elegible;
- stale se distingue de unknown;
- explain-route puede leer y mostrar este estado;
- tests con cuentas heterogéneas.

## Fase B — selección explicable por cuenta

Cada decisión importante debe producir razones estructuradas:

- provider/account/model elegido;
- alternativas elegibles;
- alternativas descartadas;
- motivo de descarte o degradación;
- cuota/reset/cooldown/health relevantes;
- policy/intent que afectó el score.

### Criterios de aceptación

- API/dashboard responde "¿por qué se eligió esta ruta?";
- inputs deterministas producen salida determinista;
- no se filtran secretos;
- el request hot path no ejecuta probes externos.

## Fase C — telemetría de valor

Medir valor mensual sin inventar ahorros monetarios:

- requests por provider/account/model;
- fallbacks automáticos;
- interrupciones absorbidas;
- porcentaje resuelto sin coste API adicional cuando sea demostrable;
- capacidad usada antes del reset/vencimiento cuando sea conocible;
- requests desviados desde cuota escasa;
- coste real conocido;
- coste estimado sólo con fuente de precio auditable;
- ahorro frente a baseline sólo cuando la baseline fue configurada y es defendible.

### Criterios de aceptación

- toda cifra monetaria indica si es real o estimada;
- nunca convertir arbitrariamente una suscripción en "USD ahorrados";
- si no existe precio fiable, mostrar capacidad/uso recuperado;
- dashboard mensual utilizable para justificar renovación.

## Fase D — frontera de entitlement

Agregar una capa de capacidades sin acoplar el router a un procesador de pagos concreto.

Capacidades candidatas:

- fabric.advanced_routing
- fabric.multi_account_policy
- fabric.intent_profiles
- telemetry.advanced
- policy.budget
- policy.quota_expiry_preference

### Requisitos

- Community sigue funcionando sin entitlement Pro;
- perder Pro no bloquea datos/configuración;
- interfaz de entitlement reemplazable;
- comportamiento offline/local definido;
- no introducir artificialmente fallos o lentitud en Community.

## Fase E — beta cerrada

Cohorte objetivo: usuarios que conecten al menos dos recursos distintos.

Medir durante una semana:

- cuántas veces evitó cambios manuales;
- cuántas interrupciones absorbió;
- cuánto tráfico reasignó desde recursos escasos;
- cuánta capacidad próxima al reset se aprovechó;
- qué funciones Pro echarían de menos al volver a Community.

Gate de cobro inicial:

- evidencia repetible de valor;
- explain-route comprensible;
- sin pérdida de configuración al quitar Pro;
- onboarding multi-proveedor simple;
- métricas suficientes para que el usuario pueda explicar por qué paga.

## No objetivos

- no bundled inference;
- no reventa de tokens en v0;
- no fork de OpenCode dentro de este repo;
- no control plane cloud obligatorio;
- no colección de secretos;
- no degradación artificial de Community;
- no lógica diseñada para eludir límites o términos de un proveedor.

## Cliente compañero

Un cliente basado en OpenCode puede existir como producto independiente y actuar como superficie de adquisición/UX.

Debe:

- funcionar sin 9router;
- detectar 9router cuando esté disponible;
- consumir /v1/models y endpoints compatibles;
- delegar toda la lógica Fabric al router;
- evitar duplicar cuotas, scoring, health y políticas.

## Resultado esperado

La prueba de valor no es "tenemos paywall".

La prueba es:

> Un usuario con varios recursos de IA trabaja durante una semana y puede mostrar, con datos, que Fabric redujo cambios manuales, absorbió fallos o aprovechó mejor capacidad que de otro modo hubiera quedado ociosa.


## Base técnica existente encontrada

La implementación no parte de cero. Antes de crear estructuras nuevas, reutilizar y generalizar estas piezas existentes:

- `internal/codexquota/quota.go`: parser común de ventanas Codex con porcentaje usado/restante y `ResetAt`.
- `internal/handlers/chat/codex_quota.go`: cache de cuota por conexión, ventanas de sesión/semanal y bloqueo hasta reset.
- `internal/handlers/chat/antigravity_quota.go`: cuota por conexión/modelo, ventanas semanales/sesión y strike breaker para 429 de cuota.
- `internal/db/accounts.go`: `rateLimitedUntil`, model locks, backoff y cooldown por conexión.
- `internal/usagetracker/tracker.go`: actividad por modelo/cuenta y requests recientes.
- `internal/controlplane/sync/accounts.go`: sincronización de cuentas al registry/control plane.

### Dirección de refactor

No crear un tercer tracker de cuotas paralelo.

Crear una abstracción normalizada de capacidad en/control plane y **adaptadores** desde las señales actuales de Codex, Antigravity y futuras fuentes. La capa común debe distinguir:

- capacidad conocida vs desconocida;
- fresca vs stale;
- agotada vs en cooldown;
- account-level vs model-level vs shared-scope;
- señal observada por API vs señal inferida por error.

Las implementaciones provider-specific siguen siendo responsables de obtener/interpretar la señal nativa. Fabric sólo consume una vista normalizada para scoring, explain-route y políticas.

Primer objetivo de ingeniería recomendado:

> Exponer un `CapacitySnapshot` provider-agnostic generado a partir de la lógica ya existente, sin cambiar todavía el algoritmo de routing.

Eso permite agregar tests y observabilidad antes de modificar decisiones de producción.
