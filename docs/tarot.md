# 타로 비교 모듈

**결론:** API 키 없이 같은 카드·정역·배치로 Tarotoo 기본 자료와 Arcanite 조립 자료를 비교할 수 있다. 78장씩과 11개 배치를 실제 Go 코드에 연결했다. 출력은 출처가 있는 자료와 LLM용 프롬프트이며, 자동으로 작성된 상담 답변이나 적중률 결과가 아니다. Arcanite의 미작성·누락 자료는 숨기지 않고 보고한다.

## 구현 범위

| 조건 | 같은 입력 | 실제 사용하는 자료 |
|---|---|---|
| `tarotoo_baseline` | 질문, 카드 ID, 정역, 위치 이름 | 해당 방향의 기본 뜻 + 지원되는 질문 영역 뜻 |
| `arcanite_assembled` | 동일 | 기본 뜻 + 위치별 뜻 + 질문 영역 뜻 + 실제 정의된 카드 관계 |

두 조건은 **자료와 구조를 동시에 바꾼다.** 답변 품질 차이가 생겨도 구조만의 효과로 결론 내릴 수 없다. 구조 효과를 분리하려면 같은 자료에서 위치/관계 부분만 제거한 추가 조건을 설계해야 한다.

## 호출 계약

```go
engine, err := tarot.New()
if err != nil { /* embedded dataset error */ }
result, err := engine.Compare(tarot.Request{
    Question: "상대에게 다시 연락하기 전에 어떤 점을 확인해야 할까?",
    QuestionType: "love",
    Spread: "situation-action-outcome",
    Cards: []tarot.Draw{
        {CardID: 6},                  // The Lovers, upright
        {CardID: 13, Reversed: true}, // Death, reversed
        {CardID: 21},                // The World, upright
    },
})
```

- `QuestionType`: `general`(기본), `love`, `career`, `spiritual`.
- `Spread`: `single-focus`(기본), `past-present-future`, `mind-body-spirit`, `situation-action-outcome`, `five-card-cross`, `four-card-decision`, `relationship-spread`, `horseshoe-traditional`, `horseshoe-apex`, `celtic-cross`, `year-ahead`.
- 카드 순서가 배치 위치 순서다. 배치와 정확히 같은 장수를 입력해야 한다.
- `CardID`: Tarotoo의 0~77. 전체 이름과 대응 Arcanite ID는 `engine.Cards()`.
- 배치별 위치와 설명은 `engine.Spreads()`.
- 잘못된 ID·중복 카드·장수 불일치·알 수 없는 배치·빈 질문은 오류다.
- 프롬프트: `result.Baseline.Prompt`, `result.Enhanced.Prompt`.
- 출력 `mode`는 항상 `evidence_only`. 실제 LLM 호출 여부는 호출하는 상위 계층에서 별도로 기록해야 한다.

## 실행한 검증

```bash
go test ./internal/tarot -count=1
```

검사 항목:

- 두 자료의 78장 ID/이름 대응과 Tarotoo 원문 SHA-256.
- Arcanite 카드 78개 + 배치 파일 1개의 SHA-256.
- 78장 × 정역 2가지 × 배치 11개 = **1,716개 비교 요청**의 조립. 각 배치의 모든 위치를 돌아가며 확인한다.
- 역방향 의미와 연애 맥락이 실제로 바뀌는지, 과거 위치가 과거 해석을 선택하는지.
- `question_contexts.*` 배치 경로를 올바르게 조회하는지.
- 누락 위치의 명시적 기본 뜻 fallback, 자리표시자 제외, 한쪽 방향에만 있는 관계 보존.
- 동일 입력 출력 재현성, 입력/카탈로그 복사, 유효하지 않은 요청 거부.

이 검사는 **자료 연결과 프로그램 동작**을 검증한다. 영어 원문의 해석 타당성, 한국어 상담 품질, 사용자 만족도, 미래 사건 적중률은 검증하지 않는다.

## 실제 데이터에서 발견한 점

| 기계적으로 센 항목 | 결과 |
|---|---:|
| 카드 | 78 |
| 배치 | 11 |
| 원문 관계 항목 | 1,379 |
| 미작성/빈 관계 항목 | 676 |
| 존재하지 않는 대상 ID를 참조하는 관계 항목 | 23 |

마지막 두 수치는 겹칠 수 있다. 예를 들어 원문 `the_wheel_of_fortune`은 실제 카드 ID `wheel_of_fortune`과 다르다. 앱이 임의로 고치지 않고 누락으로 집계한다. 현재 관계 규칙은 카드 ID만 보고 선택하므로 **역방향·배치까지 반영한 관계 규칙이라고 표시하면 안 된다.**

카드에 따라 “상대는 이렇게 생각한다”처럼 단정적인 원문도 있다. 프롬프트는 이를 상징 자료로 취급하고 실제 사연·상대의 마음·미래 사건의 증거로 쓰지 않도록 제한한다. 이 지시만으로 모델의 준수가 보장되지는 않으므로 답변 평가는 따로 필요하다.

## 다음 품질 시험

같은 모델·모델 버전·공통 지시·출력 길이를 고정해 두 프롬프트를 실행한다. 답변의 조건 이름을 가리고 사람이 비교한다. 카드/정역/위치 오독, 원문에 없는 조합 창작, 미제공 사건 삽입, 상대 마음 단정, 명시적 거절 무시를 먼저 집계하고 공감도는 별도로 받는다. `676`이라는 누락 수치를 답변 오답 수나 미래 예측 실패 수로 환산해서는 안 된다.

실제 사건 예측을 시험한다면 예측과 기한을 결과 발생 전에 저장하고, 카드 없이 동일 상황만 사용한 대조 조건 및 사건의 기본 발생률과 비교해야 한다. 이 모듈은 그런 미래 추적 자료를 생성했다고 주장하지 않는다.

## 출처와 권리

- Tarotoo: https://github.com/Tarotoo-com/tarotoo-tarot-dataset/tree/83202267973950eb5bf034f144b9405db9cb1e7e — MIT, `third_party/tarotoo/LICENSE`.
- Arcanite: https://github.com/katelouie/arcanite/tree/c025b97c49e564e16019cafba381375fd1697a17 — MIT, `third_party/arcanite/LICENSE`.
- 카드 이미지는 포함하지 않았다. 출처 원문 고지와 저작권 고지는 유지한다.

## 서버 선택 세션 구성 요소

`NewSession`은 암호학적 난수로 78장 순서를 중복 없이 섞고 정·역방향을 미리 결정한다. 서버는 전체 세션을 저장하고 덱 내용을 클라이언트에 공개하지 않은 상태에서 위치만 받아 `Select`로 선택한다. 완성된 결과는 `SessionRequest`로 같은 카드·방향을 재사용한다. PostgreSQL 설정 시 `POST /v1/tarot/sessions`, `POST /v1/tarot/sessions/{id}/select`, `GET /v1/tarot/sessions/{id}`가 인증된 사용자 범위로 이를 저장·재조회한다. 선택은 행 잠금으로 직렬화되며 API는 덱을 공개하지 않는다. DB가 없으면 API는 503을 반환한다. 구매·상세풀이 연결은 아직 없다. 타로 출처의 텍스트와 이미지 권리는 별도 검토가 필요하며 이미지 제공은 구현하지 않았다.

## 선택한 카드의 무료 요약과 상세

카드를 모두 선택한 뒤 인증된 `POST /v1/tarot/sessions/{id}/reading`을 호출한다. 본문에 카드를 보내지 않는다. 서버는 인증된 사용자가 저장한 세션의 질문·배치·카드·정역방향을 읽어 풀이의 고정 근거로 사용한다. 미완성 세션은 409, 다른 사용자의 세션은 404, 모델 미설정은 503이다. 서버가 세션 ID로 요청 키를 만들므로 같은 세션을 다시 요청하면 기존 풀이를 재사용한다. 응답의 풀이 ID로 `/v1/readings/{id}`에서 저장된 요약을 읽고, 서버 권한이 있을 때 `/v1/readings/{id}/detail`로 상세를 요청한다. 실제 결제 검증은 별도 미완료 항목이다. 생성이 중단되어 비용이 불확실한 경우 재호출하지 않고 기존 생성 상태 정책을 따른다.

제품 풀이 API의 `/v1/readings` 및 `/v1/reading-jobs`는 클라이언트가 고른 카드의 타로 생성 요청을 거부한다. 연구용 비교 API는 여전히 임의 카드의 근거 비교를 제공하지만, 이는 제품의 저장된 선택이나 구매 결과가 아니다. `/v1/reading-jobs`에서도 일운은 저장된 프로필을 우회하지 않도록 거부하며 `/v1/daily`를 사용한다.

## 제품 기본 자료 (2026-10-30)

제품은 Arcanite 하나를 기본 자료로 쓴다(`tarot-product-v1`). 세션의 카드 설명, 무료 요약, 상세풀이가 같은 근거를 쓰며, 이전처럼 카드 설명은 Tarotoo·풀이는 Arcanite로 갈리지 않는다. 카드 쌍 관계는 제품에서 제외하고 `enhanced` 비교 조건에만 남겼다. 누락 처리와 출처 간 차이 정책은 [해석 자료와 제품 기본 정책](interpretation-sources.md)에 있다. 위 비교 모듈 설명은 내부 평가용 두 조건에 대한 것이다.
