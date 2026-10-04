# Licencias beta — diseño y estado

Fecha: 2026-10-04

## Estado real

El sistema de licencia/entitlement todavía no está implementado en `main`.

La separación Community/Pro y el plan de capacidades ya están definidos. Si la prioridad es repartir betas externas, la licencia beta debe pasar a ser el siguiente bloque de implementación.

## Por qué necesitamos una licencia beta antes de repartir binarios Pro

Si se entrega una compilación con todas las funciones Pro activas y sin gate, esa copia puede seguir funcionando con esas funciones aunque el producto cambie después.

Para probar con grupos externos hay que separar:

- Community, que debe seguir siendo útil;
- capacidades experimentales Pro, habilitadas durante un período controlado.

## Lo mínimo necesario

No hace falta tener pagos, dLocal, Stripe ni un servidor comercial completo.

La primera versión puede usar una licencia firmada:

```text
license_id
channel = beta
issued_at
expires_at
features
optional tester id
signature
```

Ejemplo conceptual:

```json
{
  "license_id": "beta-00127",
  "channel": "beta",
  "expires_at": "2026-11-30T23:59:59Z",
  "features": [
    "fabric.advanced_routing",
    "fabric.multi_account_policy",
    "telemetry.advanced"
  ]
}
```

La aplicación verifica la firma con una clave pública embebida. La clave privada que emite licencias no se distribuye.

Esto permite entregar betas que vencen, renovar sin recompilar, habilitar funciones por tester y probar Free vs Pro sin depender todavía de un servidor permanente.

## Regla de confianza

Una licencia vencida no debe borrar configuración, bloquear datos, romper Community ni impedir exportar.

Sólo deshabilita las capacidades Pro correspondientes.

## Arquitectura

```text
License/Entitlement Provider
          |
          v
Feature capabilities
          |
          +-- fabric.advanced_routing
          +-- fabric.multi_account_policy
          +-- fabric.intent_profiles
          +-- telemetry.advanced
          +-- policy.budget
```

El routing no debe conocer Stripe, dLocal ni ningún procesador de pago.

Más adelante puede cambiarse:

```text
beta signed file
      ->
license server
      ->
billing backend
```

sin reescribir Fabric.

## Orden de implementación

### B0 — interfaz de entitlement
Crear una interfaz que responda si una capability está habilitada. Community es el fallback seguro cuando no hay licencia.

### B1 — licencia firmada offline
Formato versionado, expiración, capabilities, firma asimétrica, validación local y tests de manipulación/expiración.

### B2 — UI beta
Mostrar Beta activa, vencimiento, capacidades habilitadas, importación de licencia y estado Community si no hay licencia.

### B3 — herramienta interna de emisión
Herramienta separada del binario público para emitir/renovar licencias sin exponer la clave privada.

### B4 — backend comercial
Sólo después de validar interés real: login, compra, renovación, seats y procesador de pagos.

## Cuándo podemos buscar testers

Se puede empezar a reclutar testers ya.

No conviene distribuir una beta Pro externa hasta que B0, B1 y una UI B2 mínima pasen tests y el build esté claramente identificado como beta.

No se fija una fecha inventada. El gate es funcional: cuando esos puntos estén verificados, la beta puede salir.


## Nota crítica: MIT y protección real de Pro

El runtime heredado está bajo MIT. Esa licencia permite usar, modificar, redistribuir, sublicenciar y vender el software preservando los avisos requeridos.

Consecuencia técnica: si el código de las funciones Pro y el chequeo de licencia se publican completos dentro del mismo repositorio MIT, un usuario con conocimientos puede eliminar el gate y recompilar.

Por eso la licencia firmada no debe presentarse como DRM imposible de romper. Su objetivo en beta es controlar distribución normal, expiración y capacidades para testers confiables.

Para un producto comercial existen tres caminos compatibles con una base MIT:

1. **Community público + Pro privado**  
   Mantener el núcleo y las integraciones Community en el repo público. Las capacidades Pro diferenciadas viven en un módulo/repositorio privado que se incorpora a los builds Pro.

2. **Community público + servicio Pro**  
   Parte del valor Pro depende de un control plane/servicio administrado. El cliente local sigue funcionando como Community si la suscripción termina.

3. **Binario distribuido + fuente Community**  
   Distribuir builds Pro con componentes propios que no se publiquen bajo la licencia MIT del repo Community, manteniendo los avisos de las partes heredadas. Antes de comercializar, revisar la estructura exacta de licencias con asesoramiento legal.

### Recomendación para la beta

Para los primeros testers:

- repo Community sigue público;
- entitlement B0 vive como interfaz en Community;
- implementación de licencia firmada puede existir en el binario beta;
- las funciones Pro verdaderamente diferenciadas no deben quedar todas publicadas bajo MIT si queremos que la licencia tenga valor comercial real;
- no convertir el producto en DRM agresivo: al vencer, vuelve a Community sin tocar datos.

La separación de repos/módulos debe diseñarse antes del primer release Pro público.
