# Beta privada — guía inicial para testers

Fecha: 2026-10-04
Estado: borrador de trabajo

## Qué estamos probando

9router convierte varias cuentas, APIs y modelos locales en un único punto de acceso. La idea no es vender otro modelo ni obligar a usar un editor concreto.

La beta busca comprobar algo más simple: si una persona que ya usa dos o más recursos de IA puede trabajar con menos cambios manuales, menos interrupciones y mejor aprovechamiento de sus cuotas.

## A quién buscamos

La primera beta está pensada para gente que programa y ya usa más de una opción de IA: ChatGPT/Codex, Claude, Gemini/Antigravity, OpenRouter, Groq, modelos locales u otros proveedores.

No hace falta ser experto en routers. También necesitamos personas que no quieran pasar una tarde configurando JSON.

## Qué queremos que prueben

- conexión de proveedores y cuentas;
- rutas fáciles como "mejor gratuito", "rápido" o "mejor para programar";
- cambios automáticos cuando una cuenta falla o entra en cooldown;
- comportamiento con varias cuentas;
- claridad del dashboard;
- explicación de por qué se eligió una ruta;
- instalación en distintos sistemas;
- errores y lugares donde no queda claro qué hacer.

## Plataformas objetivo

- Windows;
- Linux;
- macOS;
- Android/Termux cuando el componente correspondiente lo permita;
- clientes compatibles con OpenAI.

No todas las superficies estarán necesariamente listas en la primera tanda.

## Qué no prometemos todavía

Todavía están en desarrollo:

- normalización completa de cuotas entre proveedores;
- selector avanzado por capacidad;
- explain-route completo;
- telemetría Pro;
- sistema definitivo de licencias;
- Business/Teams;
- cliente compañero de programación.

## Datos y credenciales

La dirección del producto es local-first. La beta no debe pedir al usuario que entregue sus claves al desarrollador. Las credenciales permanecen en su instalación salvo que una función futura indique de forma explícita otra cosa.

## Cómo reportar

Un reporte útil incluye:

- sistema operativo;
- versión;
- proveedor/modelo usado;
- qué esperaba que ocurriera;
- qué ocurrió;
- pasos para repetirlo;
- logs sanitizados si existen.

No publicar tokens, cookies, credenciales ni bases de datos completas.

## Qué queremos aprender antes de cobrar

1. ¿La gente entiende para qué sirve sin una explicación larga?
2. ¿Puede conectar al menos dos recursos sin ayuda?
3. ¿El routing automático evita cambios manuales de verdad?
4. ¿Los fallbacks reducen interrupciones?
5. ¿La explicación de ruta genera confianza?
6. ¿Qué función concreta echarían de menos al volver a Community?
7. ¿Pagarían entre USD 5 y 10 por esa función?

El objetivo inicial no es conseguir miles de usuarios. Es conseguir un grupo pequeño que lo use en serio.
