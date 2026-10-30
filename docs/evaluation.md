# 비교 실행과 블라인드 검토

이 프로젝트의 기본 실행은 **자료·규칙·프롬프트 비교**입니다. `not_run`은 실제 모델 답변을 생성하지 않았다는 뜻이며 답변 품질이나 적중률 점수가 아닙니다. 모델을 쓰려면 명시적 생성 옵션과 공급자 설정이 모두 필요합니다. 현재 동봉된 자동 검사는 HTTP 모의 서버와 합성 사례로 코드 동작을 확인합니다. 실사용자·전문가 평가나 미래 사건 적중 시험으로 해석하지 마세요.

## 모델 설정

`generation.ConfigFromEnv()`는 다음 값만 읽으며 호출을 시작하지 않습니다. 공급자와 모델은 기본값이 없습니다.

| 변수 | 값 |
|---|---|
| `NYANG_LLM_API_BASE` | `/chat/completions` 앞의 base URL. 예: `https://api.openai.com/v1` 또는 직접 운영하는 호환 서버 |
| `NYANG_LLM_API_KEY` | 공급자 키. 파일·터미널 출력·커밋에 넣지 않기 |
| `NYANG_LLM_MODEL` | 비교할 고정 모델 ID. 가능하면 공급자가 제공하는 고정 버전 사용 |
| `NYANG_LLM_MAX_TOKENS` | `max_completion_tokens`, 기본 1024, 허용 범위 1~32768 |
| `NYANG_LLM_TEMPERATURE` | 선택. 미지정 시 전송하지 않음. 모델이 지원하는 경우에만 지정 |

Chat Completions 호환 프로토콜을 선택한 이유는 공급자 교체가 가능한 작은 어댑터를 두기 위해서입니다. OpenAI 전용 신규 앱에 추천되는 API를 선정한 것이 아닙니다. 모델별로 `system` 메시지나 옵션 지원이 다르므로 실제 공급자와 선택 모델에서 별도 통합 검사가 필요합니다. `store:false`를 전송하지만 이것만으로 공급자 전체의 보관 정책을 통제하지는 않습니다.

어댑터는 HTTPS를 요구하고, 로컬 개발 서버만 HTTP를 허용합니다. 리다이렉트·자동 재시도를 하지 않습니다. 실패 응답 본문은 개인정보나 키가 반사될 수 있어 기록하지 않습니다. 성공 응답의 모델·fingerprint·usage·생성 설정·프롬프트 SHA256·시각을 기록합니다. 토큰 제한으로 잘린 답변은 `failed`이며 평가 대상으로 쓰지 않습니다. 동일 설정도 결과의 완전한 재현성을 보장하지 않습니다.

프로토콜 확인 출처: [공식 OpenAI Create chat completion API](https://developers.openai.com/api/reference/resources/chat/subresources/completions/methods/create), 2026-10-29 확인. `model`, `messages`, `max_completion_tokens`, `store`, `choices`, `finish_reason`, `usage`를 확인했습니다. 모델·가격은 이 프로젝트에서 자동 선택하지 않습니다.

## 호출 계약

```go
// 입력은 사주/타로/상담의 Variant를 아래 형식에 매핑한다.
inputs := []generation.Input{
    {CaseID: "C1", VariantID: "baseline", Question: question,
     CaseContext: originalRequestJSON, SystemPrompt: system, UserPrompt: baselinePrompt, Sources: sources},
    {CaseID: "C1", VariantID: "enhanced", Question: question,
     CaseContext: originalRequestJSON, SystemPrompt: system, UserPrompt: enhancedPrompt, Sources: sources},
}

// 기본 경로: 모델 호출 없이 각 행의 status=not_run.
results, err := generation.Run(ctx, inputs, nil, false)

// 사용자가 생성 실행을 선택한 경로에서만 구성하고 실행한다.
cfg, err := generation.ConfigFromEnv()
client, err := generation.New(cfg, nil)
results, err = generation.Run(ctx, inputs, client, true)
// err가 있어도 앞서 완료한 결과와 실패 행을 보존한다.

// 모든 행이 정상 완료된 경우에만 새 디렉터리에 내보낸다.
err = evaluation.Export("outputs/trial-001", results, 20261029)
```

예제의 오류 처리는 실제 호출부에서 반드시 수행해야 합니다. `Run`은 중복된 case/variant를 거부하고, 첫 실패에서 중단하여 추가 요금이 생길 수 있는 재시도를 하지 않습니다.

## 내보내는 파일과 평가 절차

`evaluation.Export`는 새 디렉터리를 만들고 기존 실험 폴더를 덮어쓰지 않습니다. 파일 권한은 0600, 디렉터리는 0700입니다. **암호화 저장소가 아니며 실제 개인정보를 수집할 용도로 만들지 않았습니다.**

| 파일 | 제공 대상 / 내용 |
|---|---|
| `blind.json` | 평가자. 사례 질문, 두 조건이 공유한 사용자 입력(`context`: 사실·카드·정역·배치 등), 무작위 A/B 답변 |
| `review.csv` | 평가자. 오류 수·문제 문장·공감·도움 평가 빈칸 |
| `answer-key.json` | 실험 관리자만. seed와 A/B→조건·자료 출처·프롬프트 hash 매핑 |
| `raw-results.json` | 실험 관리자만. 원래 프롬프트, 조건, 응답, 모델 설정 및 usage |

같은 seed와 같은 사례·조건 집합은 입력 순서에 상관없이 같은 A/B 매핑을 만듭니다. seed와 정답표를 평가자에게 주지 마세요. 구조적인 출처·모델 메타데이터는 가리지만 답변 문장에 스스로 자료 이름을 쓴 경우까지 제거하지는 않습니다. 검토 전에 중립적인 담당자가 조건을 노출하는 문장이 있는지 확인하고, 변경하면 원문과 변경 이유를 별도로 기록해야 합니다.

`CaseContext`에는 **두 조건이 공유한 원래 사용자 입력만** 넣습니다. 검색된 문서, 조건별 해석 근거, 출처·버전·모델 정보는 넣지 않습니다. 평가자는 이 맥락을 보고 답변에 없는 사실이 만들어졌는지, 지정한 카드·정역·배치를 잘못 읽었는지 판정합니다. 같은 사례의 맥락이 조건 사이에서 다르거나 JSON이 깨졌으면 내보내기를 거부합니다. JSON 객체 키 순서와 공백 차이는 무시하지만 숫자 표기는 그대로 비교하므로 원래 입력을 한 번 직렬화해서 두 조건에 공유하는 방식이 권장됩니다.

1. 입력과 자료 버전, 조건, 질문, 모델, 토큰 제한을 먼저 고정합니다. 각 사례에 최소 두 조건을 둡니다.
2. 각 조건에서 같은 사실·카드·생년월일을 사용하고 변경하려는 요소만 바꿉니다. Arcanite와 Tarotoo는 내용과 구조가 함께 바뀌므로 결과를 구조 하나의 효과로 해석하면 안 됩니다.
3. 모델 답변 생성과 블라인드 변환을 실행합니다. 서로 다른 모델/생성 설정 또는 같은 사례의 질문·공유 맥락이 섞이면 내보내기를 거부합니다.
4. 평가자는 조건을 보지 않고 미제공 사실, 상대의 마음 단정, 거절 무시, 낡은 기억 사용 등을 각각 기록합니다. 공감·도움은 별도로 채점하고 동점을 허용합니다.
5. 평가를 확정한 뒤 정답표를 열어 조건별 오류와 선호를 비교합니다. AI가 채점했다면 인간 평가와 구분합니다. 평균 점수만 높다고 효과가 검증된 것은 아닙니다.

## 실제 사건 적중은 별도 시험

연락·재회 등은 결과를 보기 전에 사건 정의와 기간, 예측 확률, 당시 사용한 정보·모델·카드를 고정해야 합니다. 나중에 실제 결과, 확인 근거, 확인 실패 여부를 기록합니다. 카드·사주 없이 상황 정보만 쓴 조건과 비교하고 누락된 결과를 실패/성공으로 임의 처리하지 않습니다. 이 저장소는 아직 그런 추적 결과를 보유하지 않습니다.

상담 조사 카드만 바꾸는 실행 가능한 쌍 비교, 예외 사례와 추가 평가 열은 [상담 조사 평가](counsel-research.md#비교-입력-준비와-사람-평가)를 참고하세요. 기본 명령은 모델을 호출하지 않습니다.

## 연속 상담 비교 (2026-10-30)

`fixtures/counsel-conversation-evaluation.json`의 가상 대화 12개를 `cmd/conversation-eval`로 준비한다. 각 사례는 같은 마지막 사용자 메시지에 대해 두 조건을 만든다.

| 조건 | 내용 |
|---|---|
| `legacy_single_question` | 이전 앱 방식. 마지막 메시지 하나만 받는 한 질문 상담 프롬프트(고정 형식, 조건부 참고 카드) |
| `conversation_v2` | 이번 방식. 최근 원문·요약·기억·연결한 풀이를 구분해 넣고 판단 후 답하는 대화 프롬프트 |
| `conversation_v2_no_references` | `--reference-ablation` 시. 대화 프롬프트에서 참고 카드만 뺀 조건 |

사례: 공감만 원하는 짧은 말, 막연한 답답함, 하루 사이 바뀐 감정, 앞선 말의 정정, 이미 해 본 방법, 원하지 않는 행동, 상대 마음 확인 요청, 썸 단계의 늦은 답장, 연락 거절 이후 생일 연락, 타로 결과 연결, 감당할 수 있는 작은 한 걸음, 요약보다 최근 원문이 많은 긴 대화.

```bash
go run ./cmd/conversation-eval                       # 모델 호출 없이 문맥 검사와 입력 준비
go run ./cmd/conversation-eval --reference-ablation  # 참고 자료 제외 조건 추가
go run ./cmd/conversation-eval --generate --max-calls 36 \
  --out outputs/conv.json --blind-out outputs/conv-blind  # 실제 호출, 비용 발생
```

- 모델 없이 실행되는 `context_checks`는 각 조건에 무엇이 들어가는지만 확인한다(정정된 값만 기억에 있는지, 요약이 늦어도 직전 메시지가 원문으로 있는지, 연락 거절 사례에 참고 카드가 빠지는지 등). 답변 품질 검사가 아니다.
- 블라인드 검토지는 도움 정도, 맥락 적합성, 없는 사실 생성, 상대 마음 단정, 실패한 조언 반복, 거절·원치 않는 행동 무시, 요청하지 않은 조언·질문, 낡은 사실 사용을 따로 적는다. 답변 길이와 인용한 자료 수는 점수에 반영하지 않도록 지시한다.
- 상대 마음 단정·없는 경험 생성·실패한 조언 반복은 문자열 검사로 해결됐다고 보지 않는다. 이 검토지로 사람이 확인해야 한다.
- 2026-10-30 현재 실제 생성과 사람 평가는 실행하지 않았다(키 없음). 결과가 생겨도 가상 대화 평가이며 실제 사용자 효과나 미래 적중률이 아니다.

