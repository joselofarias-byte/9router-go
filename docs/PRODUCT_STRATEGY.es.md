# Estrategia de producto — 9router-go fork

Fecha: 2026-10-03

## Decisión

Este fork **sí tiene sentido como producto**, pero no debe competir como “otro gateway de LLM”.

El posicionamiento diferencial es:

> **Conectá los recursos de IA que ya tenés — cuentas, APIs gratuitas o pagas y modelos locales — y el router elige automáticamente la mejor opción disponible según calidad, disponibilidad, costo, confianza y política.**

La ventaja no es tener más proveedores en una lista. La ventaja es convertir capacidad dispersa en una experiencia simple, observable y resistente.

## Qué producto estamos construyendo

El núcleo del producto es una capa de decisión por encima del data plane existente.

El data plane de 9router-go ya resuelve buena parte del trabajo pesado:

- compatibilidad OpenAI / Anthropic / Gemini y otros formatos;
- ejecutores por proveedor;
- streaming;
- fallback;
- cuentas por proveedor;
- OAuth y refresh;
- API keys;
- dashboard;
- SQLite;
- binario único;
- Termux / ARM64.

El fork agrega y debe profundizar una capa propia:

- descubrimiento de capacidad realmente utilizable;
- verificación de modelos y proveedores;
- estado de cuenta / cuota / cooldown;
- trust y quarantine;
- scoring multi-dimensional;
- políticas free-first, free-only, balanced y trusted-only;
- modelos/perfiles virtuales por intención;
- snapshots y rollback;
- fallback local;
- explainability: por qué se eligió una ruta y por qué se descartaron otras.

## Diferenciación frente a proyectos cercanos

| Proyecto | Fortaleza principal | Qué no debemos duplicar | Qué debemos aprender |
| --- | --- | --- | --- |
| 9Router / 9router-go upstream | Ecosistema, proveedores, compatibilidad y gateway | Data plane general y catálogo manual | Mantener paridad e importar proveedores sin perder Fabric |
| GoModel | Gateway/control plane general en Go, virtual models, failover, circuit breaker, budgets, observability | Rehacer un gateway empresarial genérico | Circuit breaker, presupuestos, coste observado, modelos virtuales y UX de operación |
| codex-lb | Gestión avanzada de múltiples cuentas y capacidad | Convertir el producto en un agregador exclusivo de Codex/ChatGPT | Estrategias de selección por capacidad, cooldown, ventanas de reset y visualización de cuota |
| Este fork | Orquestación adaptable de capacidad heterogénea | IDE propio, agente de coding propio o un gateway desde cero | Concentrar innovación en decisión, discovery, scoring, simplicidad y ahorro medible |

La pregunta rectora para cualquier feature nueva es:

> ¿Ayuda a decidir **qué recurso conviene usar ahora**, o simplemente reproduce algo que otro proyecto ya hace?

Si sólo reproduce infraestructura madura, integrar o adaptar. Si mejora la decisión, la fiabilidad o la experiencia simple, pertenece a este fork.

## Dos modos de producto

### Modo Fácil

El usuario elige intención, no proveedor.

Perfiles objetivo:

- **Mejor para programar**
- **Más rápido**
- **Mejor gratuito**
- **Más económico**
- **Local / privado**
- **Mejor disponible**
- **Razonamiento**
- **Contexto largo**

La implementación puede mapear estos nombres a perfiles virtuales existentes (coding-best-free, fast-free, free-best, reasoning-free, long-context-free, local) y a nuevas políticas cuando haya evidencia.

El modo fácil debe ocultar nombres técnicos salvo que el usuario quiera verlos.

### Modo Avanzado

Debe exponer:

- proveedor;
- cuenta;
- modelo;
- disponibilidad;
- cuota conocida;
- próxima ventana de reset, cuando exista;
- cooldown;
- latencia;
- tasa de éxito;
- confianza / quarantine;
- costo estimado;
- score;
- explicación de elección.

El modo avanzado no debe tener lógica separada. Debe ser una vista del mismo motor de decisión.

## Valor monetizable

El valor no se cobra por “tener un proxy”. Se cobra por una combinación de:

1. **Ahorro**: usar capacidad gratuita/barata cuando sea suficientemente buena.
2. **Continuidad**: evitar interrupciones cuando un proveedor o cuenta falla.
3. **Simplicidad**: una sola configuración para herramientas distintas.
4. **Decisión**: elegir automáticamente la ruta adecuada.
5. **Observabilidad**: saber qué se usó, por qué y cuánto costó.
6. **Administración**: políticas, equipos, límites y auditoría.

### Ediciones propuestas

Estas ediciones son hipótesis de producto; no son todavía gates de código.

#### Community

- self-hosted;
- endpoint OpenAI-compatible;
- proveedores y cuentas;
- selección manual;
- routing/fallback básico;
- modelos locales;
- métricas esenciales;
- importación/sincronización de proveedores upstream.

#### Pro

- perfiles inteligentes por intención;
- free-first y selección adaptativa;
- quota/cooldown-aware routing;
- ranking por salud real;
- discovery/verificación;
- explain-route detallado;
- métricas de ahorro;
- backups y recuperación mejorados.

#### Business

- usuarios/equipos;
- políticas por usuario/API key;
- presupuestos;
- límites;
- auditoría;
- retención configurable;
- routing policy central;
- exportación de métricas.

#### Hosted / Cloud

- control plane administrado;
- actualizaciones;
- observabilidad;
- backup;
- configuración y soporte simplificados.

La existencia de código MIT no impide monetizar instalación, hosting, soporte, operación, UX, servicios o componentes adicionales. Los avisos y condiciones de las dependencias deben preservarse.

## Métrica de valor visible al usuario

El producto debe ser capaz de mostrar evidencia, no promesas vagas.

Panel objetivo:

- solicitudes totales;
- porcentaje resuelto con rutas gratuitas;
- porcentaje resuelto localmente;
- costo estimado;
- costo evitado estimado frente a una baseline configurable;
- fallbacks automáticos;
- fallos evitados;
- tiempo/latencia por perfil;
- proveedores/cuentas en cooldown;
- motivos de descarte de rutas.

Ejemplo conceptual:

> 72% de las solicitudes se resolvieron sin costo de API.
> 316 fallbacks automáticos.
> 4 interrupciones de proveedor absorbidas.
> Ahorro estimado: USD 38,40 frente a la baseline configurada.

El ahorro debe mostrarse sólo cuando existe una baseline explícita y precios fiables. Nunca inventar precios ni presentar estimaciones como facturación real.

## Qué extraer de GoModel

Prioridad de estudio:

1. **Circuit breaker** por destino.
2. **Budgets** y guardas de costo.
3. **Virtual models** como contrato de UX.
4. **Failover configurable**.
5. **Sticky sessions** cuando aporten coherencia.
6. **Cost tracking** con tablas de precio auditables.
7. **Observability** y dashboards de operación.

No copiar un subsistema completo si el Fabric actual ya cubre el mismo problema. Extraer contratos, invariantes, casos de borde y tests.

## Qué extraer de codex-lb

Prioridad de estudio:

1. capacity_weighted;
2. relative_availability;
3. sequential_drain;
4. reset_drain;
5. selección estable entre cuentas;
6. cooldowns y circuit-breaker gates;
7. representación de ventanas de cuota/reset;
8. dashboard de disponibilidad.

Estas ideas deben generalizarse a **cualquier provider/account que publique capacidad**, no quedar atadas a Codex.

No se debe diseñar una feature con el propósito de eludir límites o políticas de un proveedor. El motor sólo orquesta recursos configurados por el operador y debe permitir políticas específicas por proveedor cuando sus condiciones lo requieran.

## Orden de ejecución

### Fase 0 — Base legal y de producto

- restaurar LICENSE y avisos heredados;
- documentar esta estrategia;
- mantener GitHub como fuente de verdad;
- evitar branding definitivo hasta terminar la búsqueda de nombre.

### Fase 1 — Capacidad y selección por cuenta

Objetivo: que el motor conozca y explique el estado de cada cuenta cuando el proveedor exponga esa información.

Agregar una representación común de:

- remaining / used;
- status: unknown, available, stale, exhausted;
- reset_at;
- cooldown_until;
- source y timestamp;
- scope cuando varios accounts comparten la misma cuota.

Criterio de aceptación: el explain-route debe poder indicar por qué una cuenta fue preferida, omitida o degradada.

### Fase 2 — Estrategias generales de cuenta

Implementar detrás de una interfaz:

- stable round robin;
- capacity weighted;
- relative availability;
- fill/drain policy;
- reset-aware preference.

Criterios:

- comportamiento determinista salvo donde se documente aleatoriedad;
- no seleccionar exhausted/cooldown cuando existe otra opción elegible;
- unknown no significa “ilimitado”;
- tests con cuentas heterogéneas;
- fallback conserva los health locks existentes.

### Fase 3 — Telemetría de costo y ahorro

Agregar:

- costo observado por request;
- precio fuente y fecha;
- baseline configurable;
- costo evitado estimado;
- separación entre costo real conocido y estimado.

Criterio: ninguna cifra monetaria se muestra como ahorro si falta una baseline auditable.

### Fase 4 — Modo Fácil

Crear una vista de producto encima de los perfiles virtuales.

El usuario debe poder resolver la configuración inicial sin entender IDs de modelos.

Criterio: una persona puede conectar al menos un recurso y elegir “Mejor gratuito”, “Mejor para programar” o “Local / privado” sin editar JSON ni memorizar nombres técnicos.

### Fase 5 — Hardening para Pro/Business

Antes de vender:

- scope/RBAC;
- presupuestos;
- límites por API key/usuario;
- auditoría;
- seguridad de backups;
- supply chain;
- benchmarks reproducibles;
- migraciones confiables;
- política de secretos.

## Reglas de arquitectura

1. **Integrate before build.**
2. No crear un IDE.
3. No crear otro agente de coding.
4. No duplicar catálogos que ya publica una fuente fiable.
5. Discovery/probes fuera del request hot path.
6. Nunca persistir secretos dentro de snapshots de Fabric.
7. Las rutas virtuales deben ser explicables.
8. Unknown/stale/exhausted son estados distintos.
9. La política “gratis” debe fallar cerrada: un precio desconocido no es gratis.
10. La compatibilidad con upstream debe mantenerse mediante sync selectivo y tests.
11. El producto no debe depender de una sola plataforma de IA.
12. Termux/ARM64 sigue siendo un entorno soportado del fork.

## Señal para decidir si el producto funciona

Antes de cobrar, debemos demostrar en uso real:

- un usuario puede configurar el router sin ayuda experta;
- el routing automático supera una estrategia simple en continuidad o costo;
- el ahorro es medible;
- el explain-route permite auditar decisiones;
- una caída o límite de un proveedor no obliga a reconfigurar manualmente;
- un modelo local puede actuar como fallback donde sea viable;
- las actualizaciones upstream no destruyen Fabric.

Si esas condiciones se cumplen, ya no estamos vendiendo “un fork”.

Estamos vendiendo **la capa que administra todos los recursos de IA del usuario como un solo sistema**.


## Posicionamiento comercial inicial — nicho que podemos ganar

Fecha de decisión: 2026-10-04.

No competir como "otro editor con IA" ni como revendedor de tokens. El cliente objetivo inicial es más estrecho y verificable:

> **Desarrolladores que ya usan o pagan dos o más recursos de IA y pierden tiempo o dinero administrando cuentas, cuotas, modelos, proveedores y fallbacks manualmente.**

La promesa comercial es:

> **Una sola capa para toda la IA que ya tenés.**

El valor de pago no es el acceso a un modelo. El usuario conserva sus propias cuentas, APIs y modelos locales. Pro cobra por convertir recursos dispersos en un sistema coordinado, observable y automático.

### Por qué un usuario pagaría a un proyecto pequeño

El producto debe reducir explícitamente la desventaja de confianza frente a proveedores grandes:

- local-first por defecto;
- claves y credenciales bajo control del usuario;
- configuración exportable;
- datos y routing funcionando localmente;
- BYOK real;
- posibilidad de seguir usando Community si se cancela Pro;
- reglas de routing explicables;
- sin dependencia obligatoria de un modelo, proveedor o editor concreto.

El mensaje no es "somos mejores que Cursor/OpenCode/Kilo en todo". Es:

> **Ellos venden o integran IA. Nosotros administramos, aprovechamos y orquestamos la IA que ya pagaste o ya tenés.**

### Diferenciación que debe ser demostrable

Fabric debe decidir usando una unidad más rica que "modelo":

`provider + account + quota + reset + cooldown + health + cost + capability + user policy + intent`.

Ejemplos de intención de usuario:

- Mejor para programar.
- Máxima calidad.
- Cero gasto adicional.
- Rápido.
- Local / privado.
- Reservar Claude.
- Usar primero cuotas que vencen.
- Mantener presupuesto mensual.
- Evitar cuentas en cooldown.
- Continuar trabajando aunque cambie el proveedor.

La selección debe ser auditable: cada decisión importante debe poder responder "por qué se eligió esto" y "por qué se descartó aquello".

### Edición Community — gratuita y útil

Community no debe ser una demo mutilada.

Debe incluir:

- gateway local;
- endpoint OpenAI-compatible;
- BYOK;
- modelos locales;
- proveedores;
- selección manual;
- routing/fallback básico;
- métricas esenciales;
- configuración exportable;
- interoperabilidad con clientes externos.

Objetivo: generar confianza, comunidad y adopción sin coste marginal de inferencia para el proyecto.

### Edición Pro — hipótesis de lanzamiento: USD 7,99/mes

Rango de validación inicial: USD 5–10/mes.

Pro vende automatización y optimización, no tokens. Candidatos:

- Fabric completo;
- multi-account avanzado;
- quota/reset/cooldown-aware routing;
- perfiles por intención;
- selección adaptativa;
- políticas "usar primero lo que vence";
- presupuestos y prioridades;
- health/trust/quarantine avanzados;
- explain-route detallado;
- estadísticas y reglas avanzadas;
- estimación auditable de coste evitado cuando exista baseline;
- sincronización opcional entre dispositivos;
- perfiles compartibles;
- backups/configuración avanzada.

No incluir inferencia propia en la primera versión Pro. Esto mantiene bajo el coste marginal y permite validar disposición a pagar antes de financiar capacidad de modelos.

### Business

Business monetiza administración, no el editor:

- equipos y roles;
- políticas centrales;
- proveedores/modelos permitidos;
- límites y presupuestos por usuario/API key;
- auditoría;
- SSO cuando exista demanda;
- configuración compartida;
- retención y exportación;
- soporte y SLA según plan.

### Métrica de renovación

El dashboard debe demostrar valor mensual. Métricas candidatas:

- cuota utilizable recuperada;
- porcentaje de tráfico resuelto sin coste API adicional;
- requests desviados desde recursos escasos hacia abundantes;
- fallbacks automáticos;
- interrupciones absorbidas;
- tiempo ahorrado en reconfiguración;
- consumo por cuenta/proveedor;
- coste real conocido;
- coste evitado sólo cuando exista baseline fiable.

Nunca convertir una suscripción en "USD ahorrados" mediante una equivalencia inventada. Cuando no exista precio auditable, mostrar uso aprovechado o capacidad recuperada.

### Objetivo comercial inicial

No buscar mercado masivo. Validar primero una cohorte pequeña de usuarios intensivos multi-proveedor.

Hitos sugeridos:

1. 20 usuarios activos que conecten al menos dos recursos distintos.
2. 10 usuarios que usen routing automático durante una semana.
3. 10 entrevistas de renovación: qué valor concreto echarían de menos al volver a Community.
4. Primeros 20 clientes Pro.
5. Llegar a 200 clientes Pro antes de ampliar el producto hacia servicios de inferencia propios.

A USD 5/mes, 200 clientes = USD 1.000 MRR.
A USD 7,99/mes, 200 clientes = USD 1.598 MRR.
A USD 10/mes, 200 clientes = USD 2.000 MRR.

Estas cifras son ingresos brutos recurrentes, antes de comisiones, impuestos y costes operativos.

## Cliente compañero de programación

La regla "no crear un IDE" aplica al repositorio `9router-go`: no debe incorporar ni reimplementar un IDE/agente de coding dentro del router.

Sí se permite un **producto compañero independiente**, preferentemente basado en un proyecto maduro y permisivamente licenciado (por ejemplo OpenCode), con estas reglas:

- repositorio y ciclo de release separados;
- puede funcionar sin 9router mediante BYOK/proveedores directos;
- detecta 9router y lo ofrece como integración preferente;
- consume `/v1/models` y endpoints compatibles;
- no duplica Fabric;
- no almacena la lógica de cuotas/routing que pertenece a 9router;
- Android/Termux puede tratarse como plataforma de primera clase si la base técnica lo permite;
- las mejoras genéricas deben upstream-earse o mantenerse como plugins/adaptadores cuando sea viable.

El cliente compañero es una superficie de adquisición y UX. **Fabric/9router sigue siendo el activo diferencial y monetizable.**
