# Go 기반 제품 연결 실험

## 지금 가능한 범위

기존 원본 엔진을 유지하고 Go가 사용자별 입력·기억·요약·상세 결과와 접근 권한을 관리합니다. UI 없는 로컬 연구 API입니다. 사주·일운은 고정된 OhMySaju TypeScript 빌드를 Node에서 호출하고, 타로는 공개 JSON 자료를 Go에서 조립합니다. 궁합은 원본 두 명식을 함께 제공하는 참고자료 단계이며 별도의 검증된 궁합 점수 엔진은 아닙니다.

무료 요약과 잠긴 상세풀이의 흐름을 구현했습니다. 실제 결제, 영수증 검증, 구독, 후속 질문 횟수 차감은 아직 연결하지 않았습니다. `--test-unlocks`는 **테스트용 해제**이고 실제 구매 증명이 아닙니다. 모델이 설정되지 않으면 가상의 답변을 만들지 않고 생성 요청이 503으로 끝납니다.

| 사용자 기능 | 현재 연결 | 한계 |
|---|---|---|
| 내 사주·연애사주 | `saju` → OhMySaju 원본 명식 → 선택한 자료 → AI 요약/상세 | 양력·정확한 출생시간만 지원 |
| 오늘의 운세 | `daily` → 출생 원국 + 지정 날짜 일진 | 날짜를 명시해야 함. 임의 점수 없음 |
| 두 사람 궁합 | `compatibility` → 두 명식·제공된 관계 상황 | 실제 궁합 점수/미래 사건 예측 엔진 아님 |
| 타로 | `tarot` → 고정된 카드·정역·배치·의미 JSON | 카드 선택 UI 없음. 입력 카드 재사용 |
| 연애 상담 | `counsel` → 현재 질문 + 선택적 확인된 개인 기억 + 참고 카드 | 참고 카드 3주제. 치료/사건 적중률 검증 없음 |
| 보관함 | `GET /v1/readings` 및 개별 조회 | 단일 프로세스 비공개 로컬 JSON |

`variant: baseline/enhanced`는 자료 조합 비교용이며 품질 등급이나 가격 등급이 아닙니다. 운영에 쓸 조합은 실제 답변을 평가해 선택해야 합니다. 유료 상세는 동일한 조합·사실·카드·원국으로 설명 범위를 확장합니다.

## 모델 없이 자동 검증

```bash
go test -race ./...
go vet ./...
SAJU_INTEGRATION=1 go test -count=1 ./internal/saju
node scripts/engine-regression.mjs
python3 scripts/validate.py
```

통합시험은 로컬에 원본 빌드가 준비되어 있어야 합니다. `scripts/setup-saju.sh`로 고정 원본과 의존성을 설치할 수 있습니다. 네트워크 호출 없는 기본 시험과 원본 엔진 통합시험을 구분합니다.

요약→잠김→테스트 해제→상세→재열람 흐름은 Go의 HTTP 모의 공급자로 시험합니다. 이는 모델이 좋은 한국어 답변을 작성했다는 뜻이 아닙니다. 모델 사용량 값도 모의 시험에서는 고정 예제입니다. 실제 모델 호출은 아래 설정이 있을 때만 일어납니다.

## 실제 모델로 로컬 흐름 시험

공급자 URL·키·모델·토큰 상한을 환경변수에 설정합니다. 예시 `.env.example`은 자동 로드되지 않습니다. 개인 실사연 대신 합성 예제로 먼저 시험하세요.

```bash
export NYANG_API_TOKEN="$(go run ./cmd/nyangpuli token)"
export NYANG_USER_ID=local-demo
# NYANG_LLM_API_BASE, NYANG_LLM_API_KEY, NYANG_LLM_MODEL을 직접 설정
# 상세 답변이 잘리지 않도록 선택 모델에 맞게 NYANG_LLM_MAX_TOKENS 설정
go run ./cmd/nyangpuli serve --enable-generation --test-unlocks
```

다른 터미널에 같은 API 토큰을 설정하고:

```bash
curl http://127.0.0.1:8080/v1/readings \
  -H "Authorization: Bearer $NYANG_API_TOKEN" \
  -H 'Idempotency-Key: synthetic-counsel-001' \
  -H 'Content-Type: application/json' \
  --data-binary @fixtures/reading-counsel.json
```

반환된 `id`를 아래 변수에 넣습니다.

```bash
NYANG_READING_ID='응답의 실제 id'
# 해제 전에는 402. 상세 모델 호출도 일어나지 않음.
curl -X POST "http://127.0.0.1:8080/v1/readings/$NYANG_READING_ID/detail" \
  -H "Authorization: Bearer $NYANG_API_TOKEN"

# --test-unlocks를 켠 실험 서버에서만 작동. 실제 과금 없음.
curl -X POST "http://127.0.0.1:8080/v1/dev/readings/$NYANG_READING_ID/unlock" \
  -H "Authorization: Bearer $NYANG_API_TOKEN"

curl -X POST "http://127.0.0.1:8080/v1/readings/$NYANG_READING_ID/detail" \
  -H "Authorization: Bearer $NYANG_API_TOKEN"

curl http://127.0.0.1:8080/v1/readings \
  -H "Authorization: Bearer $NYANG_API_TOKEN"
```

요약과 상세 생성은 각각 별도의 모델 호출입니다. 성공 결과를 재열람하거나 같은 요청 키로 재시도할 때는 새로 생성하지 않습니다. 동일 키에 다른 질문을 보내면 409입니다. 공급자 오류나 취소처럼 비용 발생 여부를 알 수 없는 경우 `summary_unknown` 또는 `detail_unknown`으로 저장합니다. 응답을 저장하기 전 종료된 `pending`도 불확실하게 취급해 같은 요청을 재호출하지 않고 409를 반환합니다. 별도의 운영자 조정·공급자 조회 기능은 아직 없으며, 과금의 정확히 한 번 보장은 아닙니다. 생성 모델과 프롬프트 해시는 공급자가 제공한 경우 결과에 기록합니다.

## 서버가 고정하는 정보

최초 요약 시점에 질문·원국/카드·확인된 기억·소스 버전이 들어 있는 생성 입력을 저장합니다. 상세 생성은 새로 명식을 계산하거나 카드/현재 기억을 다시 읽지 않고 저장한 입력과 요약을 사용합니다. 사용자가 다른 상황으로 다시 상담하려면 새 요청 키로 새 결과를 만듭니다.

`use_memory:true`는 현재 상담에서만 지원됩니다. 사용자 ID는 Bearer 토큰으로 결정하며 요청 본문으로 바꿀 수 없습니다. 같은 요청을 재전송할 때 저장 기억이 달라졌어도 최초 스냅샷을 재사용합니다. 자료/프롬프트 원문은 읽기 API에 반환하지 않습니다. 요약/상세 토큰 사용량은 실제 공급자가 반환할 때만 실제 원가 측정에 사용할 수 있습니다.

같은 근거를 재사용한다고 LLM이 요약과 상세에서 의미상 모순을 만들 수 없는 것은 아닙니다. 실제 모델 시험에서 상충된 주장·새로 지어낸 사실·미확인 상대 마음 단정을 별도로 검토해야 합니다.

## 출시 전 남은 연결

- UI·사용자 로그인·운영 DB 및 계정 탈퇴 시 기억/풀이 스냅샷 함께 삭제.
- 서버에서 검증한 결제 영수증/웹훅·환불·권한 회수. 테스트 해제 기능 제거.
- 후속 질문 대화, 구매에 포함된 답변 횟수의 원자적 차감.
- 하루 무료 운세 제한, 계정별 생성 예산, 부하 제한, 동시 사용자 처리. 현재 저장소는 실험용이며 전체 서비스 임계 구역에서 생성이 직렬화될 수 있음.
- 실제 생성 원가와 무료 제공 비용 집계, 답변 품질 블라인드 평가.
- 음력·시간 미상 정책과 출생 시간대 경계 검증 확대.

Go 이식은 입력/출력 계약을 유지한 계산 어댑터 교체로 진행합니다. [엔진 회귀벡터](engine-migration.md)를 먼저 통과시키고 원본과 다른 결과를 조사한 다음 새 엔진을 선택합니다. 원본과의 일치는 전통 해석의 타당성이나 실제 예측 정확성을 입증하지 않습니다.
