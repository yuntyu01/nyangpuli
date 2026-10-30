# 해석 자료와 제품 기본 정책

2026-10-30 기준 코드에서 각 자료가 실제로 어디서 읽히고 어디에 쓰이는지 확인한 결과다. 자료가 많다는 이유로 모두 합치지 않는다. 제품은 영역마다 하나의 기본 정책을 쓰고, `baseline`/`enhanced`는 내부 평가용 비교 조건으로 남긴다. 두 조건은 무료/유료 등급이 아니다.

## 실제 사용 경로

| 자료 | 읽는 코드 | 제품 기본 정책 | 내부 비교 조건 |
|---|---|---|---|
| OhMySaju (고정 커밋 `a8c0cf6`) | `adapters/ohmysaju/*.mjs` ← `internal/saju` | 사주·일운·궁합의 계산 사실 전부 | 두 조건 모두 같은 계산 사실 |
| YueYuan 조후표 120칸 | `internal/saju/tiaohou.json` | **사용하지 않음** | `enhanced`(`ohmysaju_plus_yueyuan_tiaohou`)에서만 실험적 참고 |
| Tarotoo 카드 의미 | `third_party/tarotoo/cards.json` ← `internal/tarot` | **사용하지 않음** | `baseline`(`tarotoo_baseline`) |
| Arcanite 카드·배치 | `third_party/arcanite/*` ← `internal/tarot` | 핵심 의미, 위치별 해석, 질문 영역 해석, 배치 위치 | `enhanced`는 여기에 카드 쌍 관계를 더함 |
| BCcampus 편집 카드 3개 | `internal/counsel/cards.json` | 주제가 맞고 안전·연락 조건이 막히지 않을 때만 참고. 사용 여부를 기록 | `enhanced`(`bccampus_cards`) |
| 조사 카드(유튜브 자막·댓글 편집) | `internal/counsel/research_cards.json` | 안전·상호 대화 의사·연락 허용이 모두 확인되고 검토된 출처일 때만 참고. 사용 여부와 출처를 기록 | `enhanced`의 `RESEARCH_CARDS` |
| Mem0 | `internal/memory/mem0.go` | **사용하지 않음**. 답변 생성 문맥에 들어가지 않음 | `/v1/memory/remote`, `/v1/compare/memory`에서 명시 요청 시에만 외부 호출 |

이 표는 `internal/lab/policy.go`의 `Policies()`와 같은 내용이다. 정책 ID는 생성 입력의 `sources`에 `policy:<id>`로 기록된다.

## 영역별 기본 정책

### 타로 `tarot-product-v1`

- 기본 자료는 **Arcanite 하나**다. 세션 화면의 카드 설명(`meanings`), 무료 요약, 상세풀이가 모두 같은 `Product` 근거를 쓴다. 세션 응답에 `interpretation_policy`가 함께 온다.
- 정방향/역방향 핵심 의미, 선택한 배치 위치의 해석, 질문 영역(연애·일·영성)의 해석을 쓴다.
- 위치 해석이 비었거나 `TO BE WRITTEN`이면 같은 카드의 핵심 의미로 대체하고 `position_fallback:true`와 경고를 남긴다. 앱은 대체된 위치 해석을 따로 보여주지 않는다.
- 질문 영역 해석이 없으면 넣지 않고 경고를 남긴다. 다른 출처나 모델로 빈칸을 채우지 않는다.
- 카드 쌍 관계는 제품에서 뺐다. 1,379개 중 676개가 미작성이고 23개는 존재하지 않는 카드 ID를 가리키며, 정역방향·위치를 조건으로 쓰지 않는다. 관계 자료는 `enhanced` 비교 조건에만 남는다.
- Tarotoo와 Arcanite의 의미가 다를 때 둘을 합치거나 어느 쪽이 맞다고 확정하지 않는다. 제품은 Arcanite 원문만 보여주고, 차이는 비교 실험에서만 다룬다.
- 정책 도입 전에 `enhanced`로 만든 타로 풀이는 같은 세션에서 다시 요청하면 그 기록을 그대로 돌려준다. 새 카드나 새 생성은 만들지 않는다. 그 풀이의 카드 설명 문장은 같은 Arcanite 원문이고 카드 쌍 관계만 추가로 들어 있었다.

### 사주·일운·궁합 `saju-product-v1`

- OhMySaju 계산 사실만 쓴다. YueYuan 조후표는 원저자가 120칸을 고전 원문과 전수 대조하지 않았다고 밝힌 실험 자료이므로 제품 기본 답변에 넣지 않는다.
- 조후표를 더했을 때 답변이 나아지는지는 아직 평가하지 않았다. `enhanced` 조건으로 같은 입력을 비교하는 별도 평가 대상이다.
- 일운(`/v1/daily`)은 처음부터 OhMySaju 단독(`baseline`)을 써 왔고 같은 자료 조합이라 경로를 바꾸지 않았다.
- 출생 시간 미상은 지원하지 않으며 임의 시간을 넣지 않는다.

### 상담 `counsel-product-v1`

- 공통 상담 지침이 기본이다. 외부 참고 카드는 적용 조건이 맞을 때만 넣고, 넣었는지와 카드 ID·버전을 생성 입력 출처에 남긴다.
- 상담 답변 구성의 상세 정책은 [상담](counsel.md)을 참고한다.

## API

- `POST /v1/readings`, `POST /v1/reading-jobs`의 `variant`는 제품에서 `product`를 쓴다. `baseline`/`enhanced`는 내부 비교용으로 계속 받는다.
- `/v1/compare/*`는 기존처럼 두 비교 조건을 함께 돌려준다.
- 모바일 앱은 사주·궁합·상담 요청에 `product`를 보낸다. 타로 풀이는 서버가 세션에서 `product`로 만든다.

## 남은 평가

- YueYuan 조후표, Arcanite 카드 쌍 관계, BCcampus·조사 카드가 실제 답변을 낫게 하는지는 평가하지 않았다. 실제 모델 호출 0회다.
- 원문은 한국어 전문가 검수를 거치지 않았다.
- 카드 이미지와 텍스트의 권리 검토는 [NOTICE](../NOTICE.md)와 각 라이선스를 따른다.
