package bridge

import (
	"context"
	"errors"
	"strings"
	"testing"

	"quasarbridge/internal/llm"
)

type completionFunc func(context.Context, []llm.Message, bool) (string, error)

func (f completionFunc) Complete(ctx context.Context, m []llm.Message, b bool) (string, error) {
	return f(ctx, m, b)
}

type homeFunc func(context.Context, string) (string, error)

func (f homeFunc) Handle(ctx context.Context, s string) (string, error) { return f(ctx, s) }

func TestEngineRoutesHomeComplexAndYandexWithoutDuplicateBackend(t *testing.T) {
	for _, tc := range []struct{ text, route string }{{"Включи свет в кабинете", "home"}, {"Напиши код сортировки на Go", "llm"}, {"Какая завтра погода", "yandex"}} {
		t.Run(tc.route, func(t *testing.T) {
			homeCalls, answers := 0, 0
			engine := Engine{Home: homeFunc(func(ctx context.Context, s string) (string, error) { homeCalls++; return "Включено", nil }), LLM: completionFunc(func(ctx context.Context, m []llm.Message, structured bool) (string, error) {
				if structured {
					return `{"route":"yandex"}`, nil
				}
				answers++
				return "Ответ модели", nil
			})}
			got, err := engine.Resolve(t.Context(), tc.text)
			if err != nil || got.Route != tc.route {
				t.Fatalf("result=%+v err=%v", got, err)
			}
			if (homeCalls > 0) != (tc.route == "home") || (answers > 0) != (tc.route == "llm") {
				t.Fatalf("wrong backends: home=%d llm=%d", homeCalls, answers)
			}
		})
	}
}

func TestHomeFailureIsNotRetriedInYandex(t *testing.T) {
	e := Engine{Home: homeFunc(func(context.Context, string) (string, error) { return "", errors.New("readback failed") }), LLM: completionFunc(func(context.Context, []llm.Message, bool) (string, error) {
		t.Fatal("home failure fell through to LLM")
		return "", nil
	})}
	r, err := e.Resolve(t.Context(), "Выключи свет в кабинете")
	if err == nil || r.Route != "home" {
		t.Fatalf("unsafe fallback=%+v %v", r, err)
	}
}

func TestRefusalDetectionAvoidsBenignMention(t *testing.T) {
	for _, text := range []string{"Я не могу помочь с этим запросом.", "Извините, я не могу обсуждать эту тему.", "Я не поняла ваш вопрос."} {
		if !IsRefusal(text) {
			t.Fatalf("missed refusal %q", text)
		}
	}
	if IsRefusal("Автор написал: я не могу помочь с этим запросом. Разберём эту цитату.") {
		t.Fatal("quoted refusal classified as own refusal")
	}
	if IsRefusal(strings.Repeat(" ", 3) + "Сегодня хорошая погода.") {
		t.Fatal("benign answer rejected")
	}
}

func TestObservedAliceRefusalVariants(t *testing.T) {
	for _, s := range []string{"Увы, но я не могу понять, где это на планете Кракозябра.", "Извините, я не знаю, где это.", "Я не поняла вопрос."} {
		if !IsRefusal(s) {
			t.Errorf("missed refusal: %q", s)
		}
	}
}
