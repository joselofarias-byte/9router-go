# Auditoría multi-modelo del código — plan de trabajo

Fecha: 2026-10-04

## Objetivo

Hacer una revisión profunda de `joselofarias-byte/9router-go` con modelos diferentes y comparar hallazgos, sin asumir que un solo modelo detectará todos los problemas.

GitHub sigue siendo la fuente de verdad. Ningún agente debe modificar `main` directamente.

La auditoría no está limitada a modelos gratuitos. Las cuotas ya incluidas o semanales existen para usarse: una cuota que llega al reset sin aprovecharse en trabajo de alto valor es capacidad desperdiciada.

## Política de uso de modelos y cuotas

Orden por defecto para este repositorio:

1. **Claude vía Antigravity**: primera pasada profunda de arquitectura, seguridad, concurrencia, routing, cuotas y coherencia entre ramas. Usar el modelo Claude de mayor capacidad disponible en Antigravity y razonamiento alto cuando exista la opción.
2. **GPT-6 Astra / GPT-5.6 Sol / Codex especializado**: segunda opinión independiente, resolución de contradicciones y revisión de PRs de alto riesgo.
3. **Modelos especializados de programación disponibles a través de 9router**: revisiones focalizadas y contraste.
4. **Modelos gratuitos o locales**: tareas mecánicas, clasificación, documentación, tests sencillos y revisiones repetibles. Son apoyo, no una restricción del análisis.

No reservar Claude o Astra simplemente por ser recursos valiosos. Si la cuota es incluida y tiene reset periódico, priorizar trabajo importante antes del reset.

No votar por mayoría: un hallazgo se acepta sólo cuando está respaldado por código, test, reproducción o argumento técnico verificable.

## Baseline de arranque de esta auditoría

Estos SHA son el punto de partida observado el 2026-10-04. **Antes de ejecutar, refrescar los heads en GitHub y documentar cualquier cambio**.

- `main`: `3caa2a55cf7458a4182859c79cbc363b4007bf7c`
- PR #54 — integración Fabric/upstream: `f49dc983c5abd1e24147f39107af86a69802f34c`
- PR #57 — estrategias genéricas de cuentas: `3218580b1ce9b5ae3f1c59906fb3b6317b16c02a`
- PR #62 — corrección weighted rendezvous sobre #57: `7d28a66a72b157148cc0a5939b57de3d543cfc34`
- PR #63 — OpenCode Zen autenticado/fail-closed: `5f83c39757907de7bd92319be181fbdbdc78c932`

Relaciones importantes:

- #54 es la línea de integración upstream/Fabric y todavía debe re-portar paquetes `internal/controlplane/*`.
- #57 es una capa posterior de selección de cuentas.
- #62 es hijo de #57; no debe evaluarse como si fuera un PR independiente contra `main`.
- #63 corrige la falsa ruta anónima de OpenCode Zen y debe conservar el comportamiento fail-closed.

## Pasada A — Claude/Antigravity: arquitectura y riesgos sistémicos

Modelo recomendado: el Claude de mayor capacidad disponible en Antigravity.

Buscar especialmente:

- inconsistencias entre Control Plane y Data Plane;
- divergencia entre elegibilidad de modelo y elegibilidad real de cuenta;
- estados duplicados de quota/cooldown/health/trust;
- carreras, snapshots stale y TOCTOU;
- rutas que puedan saltarse fail-closed;
- exposición o cruce de secretos entre proveedores;
- cambios de upstream que rompan Fabric;
- acoplamientos que dificulten Community/Pro;
- problemas de migración/rollback;
- huecos de tests en decisiones críticas;
- coherencia de #54 con #57/#62 y #63;
- wiring real de feedback de resultados hacia trust/scoring;
- determinismo y explicabilidad de selección.

Salida: hallazgos priorizados P0/P1/P2 con archivo, símbolo, evidencia y test recomendado.

## Hipótesis obligatorias a probar o refutar

Estas observaciones surgieron en la pasada inicial y **no deben aceptarse por autoridad**. Claude debe demostrarlas o descartarlas con código/tests.

### H1 — capacidad/cuota llega demasiado tarde a la decisión

En `main`, `internal/controlplane/routing/policy.go` arma candidatos usando estado activo, trust, free-tier y riesgo. La selección concreta de cuenta en `internal/handlers/chat/connections.go` aplica después cooldown, model locks y caches de cuota Codex/Antigravity.

Probar si una ruta puede quedar bien puntuada/elegible en Control Plane aunque todas las cuentas ejecutables estén temporalmente agotadas o en cooldown. Si ocurre, cuantificar si sólo produce un hop fallido o si causa una decisión incorrecta observable.

Relacionar el resultado con CapacitySnapshot/#50 y el futuro wiring de #57.

### H2 — explainability de Preserve en #57/#62

En `accountstrategy.Select`, las evaluaciones se construyen antes de `preferNonPreserved`. Probar si una cuenta `Preserve=true` puede quedar reportada como `Eligible=true` aunque haya sido retirada del conjunto seleccionable por existir una cuenta no preservada.

Si se confirma, proponer el cambio mínimo para que explain-route distinga elegible de seleccionable o emita `preserved_held_back`.

### H3 — feedback de resultados hacia trust/scoring

Existe `trust.Manager.RecordObservation`, pero la pasada inicial no encontró su llamada en los archivos más probables del hot path. Hacer búsqueda repo-wide y demostrar:

- dónde se registran éxito/fallo reales, o
- que actualmente no se registran.

No declarar bug sin esa comprobación. El manifiesto de #54 ya enumera route-outcome feedback como runtime hook requerido.

### H4 — desempate aleatorio vs routing estable

`routing.Engine.SelectCandidates` premezcla candidatos antes del sort por score. Evaluar si este desempate aleatorio entra en conflicto con sticky routing, reproducibilidad de explain-route o la futura selección determinista de cuentas de #57/#62.

### H5 — integración #54 incompleta por diseño

Verificar que los paquetes Fabric listados en `docs/FABRIC_PORT_MANIFEST.md` realmente siguen pendientes en el head actual de #54 y que ningún merge posterior vuelve obsoleta esa afirmación.

## Pasada B — segunda revisión fuerte e independiente


Usar uno de los mejores modelos disponibles fuera de la primera pasada, por ejemplo Astra/Sol/Codex especializado, sobre los hallazgos P0/P1 y zonas críticas.

Candidatos de programación cuando estén disponibles:

- `codex/gpt-5.6-sol-review`
- `codex/codex-auto-review`
- `codex/gpt-5.3-codex-spark-review`
- `kimi/kimi-k2.7-code`
- `kimi/kimi-for-coding`
- `xai/grok-code-fast-1`
- `alicode/qwen3-coder-next`
- `alicode/qwen3-coder-plus`

No ejecutar todos por costumbre. Elegir según disponibilidad y valor marginal de otra opinión.

## Pasada C — modelos gratuitos/locales como apoyo

El repo incluye `scripts/free-coding-measure.sh`.

Antes de confiar en un modelo gratuito:

```bash
bash scripts/free-coding-measure.sh --tier discovery
```

Después ejecutar tareas del arnés sólo contra ids que sigan activos y clasificados correctamente.

Los candidatos gratuitos cambian con el tiempo. No convertir "gratis" en requisito para una revisión importante si existe cuota fuerte ya incluida.

## Pasada D — auditorías temáticas independientes

Asignar una preocupación por agente cuando aporte cobertura:

1. Seguridad / secretos / auth.
2. Routing y fallback.
3. Quota/cooldown/multi-account.
4. Persistencia / SQLite / snapshots / migraciones.
5. Concurrencia.
6. API compatibility OpenAI/Claude/Gemini/OpenCode.
7. Dashboard y coherencia con backend.
8. Tests faltantes y falsos positivos.
9. Licencias/entitlements y degradación Community.
10. Termux/ARM64/multiplataforma.

Un agente no debe arreglar fuera de su alcance durante la auditoría. Primero reporta.

## Formato obligatorio de hallazgo

```text
ID:
Severidad: P0 | P1 | P2
Estado: confirmado | probable-necesita-test | diseño | falso-positivo
Archivo/símbolo:
Rama/SHA:
Qué puede pasar:
Por qué:
Evidencia:
Cómo reproducir o demostrar:
Test que falta:
Cambio mínimo sugerido:
Confianza:
```

No aceptar frases genéricas tipo "podría mejorar" sin evidencia.

## Fusión de resultados

El coordinador agrupa duplicados y clasifica:

- confirmado;
- probable, necesita test;
- diseño/opinión;
- falso positivo.

Los hallazgos confirmados se convierten después en ramas/PR pequeñas. La auditoría no modifica runtime salvo orden explícita.

## Regla de privacidad y credenciales

No copiar tokens, cookies, bases de datos ni credenciales en prompts o logs.

El repositorio es auditable, pero secretos y datos de cuenta quedan fuera de cualquier contexto de modelo. Al mostrar comandos/salidas, sanear credenciales.

## Regla de ejecución en Antigravity

Leer `AGENTS.md` y `CLAUDE.md`. Usar Graphify cuando el grafo esté disponible y prefijar comandos de shell con `rtk`, según las reglas ya instaladas.

Workflow recomendado: `.agents/workflows/claude-code-audit.md`.
