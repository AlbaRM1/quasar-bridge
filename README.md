# Quasar Bridge — форк для reasoning-моделей

[![Лицензия: Apache 2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/go-1.27.3-00ADD8.svg)](go.mod)
[![Платформа](https://img.shields.io/badge/platform-Yandex%20Station%20aarch64-ffcc00.svg)](#совместимость)
[![Оригинал](https://img.shields.io/badge/upstream-skrashevich%2Fquasar--bridge-6e40c9.svg)](https://github.com/skrashevich/quasar-bridge)

Мост между Яндекс Станцией и вашей нейросетью. Вы по-прежнему говорите «Алиса»,
а программа решает, кому передать запрос: Яндексу, вашей модели или Home Assistant.
Распознавание речи и озвучивание остаются у Яндекса, поэтому интернет нужен всегда.

> **Это форк.** Оригинальный проект — [skrashevich/quasar-bridge](https://github.com/skrashevich/quasar-bridge),
> автор — **Сергей Крашевич** ([@skrashevich](https://github.com/skrashevich)).
> Весь код, документация и дизайн протокола — его работа. Здесь добавлены
> правки для провайдеров с «рассуждениями» (DeepSeek и подобные) и две
> настройки удобства. Лицензия не менялась: Apache-2.0, см. [LICENSE](LICENSE),
> [NOTICE](NOTICE) и [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES).

## Чем этот форк отличается

Пять правок поверх `v0.3.6`, каждая отдельным коммитом в ветке
[`feat/config-and-provider-compat`](https://github.com/AlbaRM1/quasar-bridge/tree/feat/config-and-provider-compat):

| Правка | Настройка | Зачем |
| --- | --- | --- |
| Совместимость с reasoning-моделями | `llm_extra_body` | DeepSeek в thinking-режиме отвечал `HTTP 400`, и любая команда с инструментами падала |
| Перенос `reasoning_content` | — | Модель может вызывать инструменты, не выключая рассуждения |
| Свой системный промпт | `system_prompt` | Промпт больше не захардкожен в коде |
| Повторный приём речи | `listen_after_reply` | После ответа модели колонка снова слушает, как штатная Алиса |
| Причина сбоя в диагностике | — | `-check-backends` больше не глотает ошибку, из-за которой её нельзя было отладить |

Подробный разбор проблемы — в разделе [Настройка под DeepSeek](#настройка-под-deepseek).

## Что умеет

- **Маршрутизация по фразам.** Музыка, погода, таймеры и будильники уходят Яндексу.
- **Своя модель** для сложных вопросов, программирования, объяснений и чтения веб-страниц.
- **Home Assistant** для умного дома, если он подключён; иначе команды остаются у Яндекса.
- **Замена отказов.** Если Алиса отвечает «не знаю», вопрос уходит вашей модели.
- **Инструменты модели:** `read_webpage`, `search_history`, `current_time`, плюс свои MCP-серверы.
- **Память диалога** на несколько реплик и понятный журнал запросов в JSONL.

ASR и TTS остаются облачными: даже локально обработанный ответ озвучивает Яндекс.

## Установка на колонку

Нужны root-права на колонке (aarch64) и менеджер пакетов
[YAM](https://github.com/nyakokitsu/yam/blob/main/docs/DEPLOYMENT.md).

1. В `settings.toml` демона `yamd` разрешите запуск программ: `allow_plugin_binaries = true`,
   и перезапустите `yamd`.
2. Соберите пакет (готовых `.yap` в этом форке нет) и установите:

```sh
python3 scripts/build-yap.py
yam install dist/quasar-bridge-<версия>-linux-arm64.yap
yam service enable quasar-bridge
```

3. Настройте `/data/opt/quasar-bridge/config.json` и `runtime.env`, затем перезапустите:
   `sh /data/quazar/yam/data/plugins/quasar-bridge/control.sh restart`.
4. Переключите речевой тракт колонки на мост: `sh .../control.sh apply` и перезагрузите колонку.

Мост работает только на loopback (`127.0.0.1:8443`), в локальную сеть не смотрит.
Полная инструкция со всеми вариантами — в [docs/guide.md](docs/guide.md).

## Настройка

Минимальный рабочий конфиг:

```json
{
  "listen": "127.0.0.1:8443",
  "mode": "hybrid",
  "tls_cert": "",
  "tls_key": "",
  "allowed_ips": ["127.0.0.1", "::1"],
  "llm_base_url": "https://api.deepseek.com",
  "llm_model": "deepseek-flash",
  "system_prompt": "",
  "listen_after_reply": true,
  "llm_extra_body": {"thinking": {"type": "disabled"}},
  "local_progress": {"enabled": true}
}
```

Ключ API — в `/data/opt/quasar-bridge/runtime.env`, файл с правами `0600`:

```sh
QUASAR_LLM_API_KEY='sk-...'
```

| Ключ | По умолчанию | Что делает |
| --- | --- | --- |
| `llm_base_url` | — | Базовый URL API; у DeepSeek допустимы оба варианта, с `/v1` и без |
| `llm_model` | — | Идентификатор модели, у DeepSeek — `deepseek-flash` или `deepseek-v4-pro` |
| `system_prompt` | встроенный | Свой системный промпт для ответов; пустая строка — оставить встроенный |
| `listen_after_reply` | `true` | Держать микрофон открытым после ответа: уточнение можно сказать без «Алисы» |
| `llm_extra_body` | `{}` | Поля провайдера в тело запроса; контракт клиента (`model`, `messages`, `tools`…) они не перетирают |
| `local_progress` | `false` | Беззвучное ожидание долгого ответа, чтобы прошивка не оборвала связь на 15 секундах |

`system_prompt` можно задать и через окружение — `QUASAR_SYSTEM_PROMPT`, удобно
для многострочного текста. Все ключи, кроме `listen_after_reply`, меняются только
перезапуском службы.

## Настройка под DeepSeek

DeepSeek (и другие модели с рассуждениями) требует возвращать `reasoning_content`
в assistant-сообщении с `tool_calls`. Локальные инструменты моста — `current_time`,
`read_webpage`, `search_history` — выполняются до первого запроса к API и формируют
такое сообщение сами, без этого поля. Ответ API:

```
HTTP 400: The `reasoning_content` in the thinking mode must be passed back to the API.
```

Поле `chat_template_kwargs.enable_thinking=false`, которое мост отправляет всегда, —
соглашение vLLM/Qwen; DeepSeek его игнорирует. Поэтому рассуждения выключаются
явно, параметрами провайдера:

```json
"llm_extra_body": {"thinking": {"type": "disabled"}}
```

Проверка после правки:

```sh
state=/data/opt/quasar-bridge
set -a; . "$state/runtime.env"; set +a
sh /data/quazar/yam/data/plugins/quasar-bridge/start.sh -check-backends
sh /data/quazar/yam/data/plugins/quasar-bridge/start.sh -query 'спроси нейросеть: какая столица Франции?'
```

Ожидается `LLM: ok; Home Assistant: disabled` и `{"Route":"llm","Answer":"Столица Франции — Париж."}`.

## Сборка из исходников

Нужен Go из [go.mod](go.mod) (1.27.3) и Python 3.11+ для пакета `.yap`.

```sh
go build -trimpath -o bin/quasar-bridge ./cmd/quasar-bridge
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -o bin/quasar-bridge-linux-arm64 ./cmd/quasar-bridge
python3 scripts/build-yap.py
```

Проверки перед установкой:

```sh
go vet ./...
go test ./...
python3 scripts/check-public-tree.py
```

## Диагностика

```sh
pkg=/data/quazar/yam/data/plugins/quasar-bridge
state=/data/opt/quasar-bridge
set -a; . "$state/runtime.env"; set +a

sh "$pkg/start.sh" -check-config       # валидация JSON без сети
sh "$pkg/start.sh" -check-backends     # проверка LLM и Home Assistant
sh "$pkg/start.sh" -route 'текст'      # какой маршрут выбран, без выполнения
sh "$pkg/start.sh" -query 'текст'      # выполнить обработчик и показать ошибку
wget -q -O - http://127.0.0.1:8443/healthz
```

`-query` печатает текст ошибки (`LLM returned HTTP 400` и подобное) — начинайте с него.
Журнал службы: `yam service logs quasar-bridge`.

## Откат

```sh
sh /data/quazar/yam/data/plugins/quasar-bridge/control.sh rollback
reboot
# затем, с компьютера:
yam service disable quasar-bridge
yam rm quasar-bridge
```

`rollback` возвращает прежний адрес речевого тракта. Пакет без этого шага удалять
нельзя: колонка останется смотреть в остановленный мост.

## Ограничения

- `max_tokens` в клиенте фиксирован (1024) и не настраивается: очень долгий ответ обрежется.
- ASR и TTS всегда облачные, офлайн-режима нет.
- Модели без поддержки `tools` и `response_format` не подойдут для инструментов и классификатора.
- Проверено на Станции 2 (aarch64, Linux 4.9) с YAM 0.1.15; 32-битные колонки не поддерживаются.

## Совместимость

| Что | Проверено |
| --- | --- |
| Колонка | Станция 2 / `Yandex Prune EVT2`, aarch64, Linux 4.9.113 |
| YAM | 0.1.15 |
| Go | 1.27.3 |
| Провайдер | DeepSeek (`deepseek-flash`, `deepseek-v4-pro`), любой OpenAI-совместимый API |
| Home Assistant | MCP-сервер с инструментами `ha_*`, необязательно |

## Благодарности

- **Сергей Крашевич** ([@skrashevich](https://github.com/skrashevich)) — автор
  Quasar Bridge: протокол, сервер, документация, пакет для YAM.
- [freequasar/quasar-go-genproto](https://github.com/freequasar/quasar-go-genproto) —
  схемы протокола Quasar/Uniproxy.
- [nyakokitsu/yam](https://github.com/nyakokitsu/yam) — менеджер пакетов для колонки.
- [homeassistant-ai/ha-mcp](https://github.com/homeassistant-ai/ha-mcp) — MCP-сервер Home Assistant.

## Лицензия

Проект распространяется по лицензии [Apache License 2.0](LICENSE).
Сведения об авторских правах — в [NOTICE](NOTICE), о сторонних компонентах —
в [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES). Форк сохраняет исходную лицензию
и указание авторства.
