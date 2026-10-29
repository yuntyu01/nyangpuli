# 냥풀이 실험실

사주·운세·궁합·타로·연애 상담을 연결하는 **Go 기반 제품 API와 조합 비교 실험실**입니다. Go가 사용자별 기억, 무료 요약, 상세 잠금, 보관함을 관리하고, 사주 계산은 기존 OhMySaju TypeScript 엔진을 유지합니다. 계산·자료 조립은 API 키 없이 실행할 수 있으며, AI 생성은 공급자를 설정한 뒤 CLI `--generate` 또는 서버 `--enable-generation`으로 켭니다.

현재는 UI 없는 로컬 연구용 코드입니다. 실제 결제는 연결하지 않았고 실제 LLM 호출·미래 적중 평가도 아직 0입니다. 자동 시험 통과는 코드와 원본 엔진 연결의 검증이며, 상담 효과나 예측 정확도의 증거는 아닙니다. [제품 흐름](docs/product-flow.md)과 [이후 Go 엔진 이식](docs/engine-migration.md)에 구현 범위와 다음 단계를 정리했습니다.

## 들어 있는 조합

| 영역 | 기본 조건 | 비교 조건 | 현재 구현 범위 |
|---|---|---|---|
| 사주 | OhMySaju 원국 | 같은 원국 + YueYuan 조후표 | 실제 계산 + 선택한 120칸 표. 신강약·격국 전체 이식 아님 |
| 오늘 운세 | 원국 + 지정 날짜 일진 | 같은 계산 + 조후 참고 자료 | 출생 시간대 정오 일진. 임의 운세 점수 없음 |
| 두 사람 궁합 | 두 명식 + 제공된 관계 상황 | 같은 두 명식 + 각자의 조후 참고 자료 | 두 명식 참고 단계. 검증된 궁합 점수 엔진 아님 |
| 타로 | Tarotoo 카드 의미 + 공통 배치 | Arcanite 정역·위치·질문·카드 관계 | 78장·11배치. 미작성 관계 제외 |
| 개인 기억 | 출처·시점 있는 확정 사실 저장소 | 선택적 Mem0 저장·검색 어댑터 | 정정·이력·사용자 격리·삭제. Mem0 실서버는 미검증 |
| 상담 | 공통 상담 지침 | 같은 상황 + BCcampus 편집 카드 | 갈등 후 대화·서운함 표현·연락 기준 3개 주제 |

타로 비교는 자료와 구조가 함께 바뀝니다. 결과 차이를 “Arcanite 구조만의 효과”라고 판단할 수 없습니다. 사주·타로 자료는 사용자와 상대의 실제 마음이나 미래 사건을 입증하는 자료가 아닙니다.

## 바로 실행

Go 1.23 이상이 필요합니다. 현재 검증에 사용한 버전은 `docs/validation-results.json`에 기록합니다. Go 코드는 외부 Go 모듈 없이 표준 라이브러리를 사용합니다.

```bash
go test ./...
go run ./cmd/nyangpuli compare -domain tarot -input fixtures/tarot.json
go run ./cmd/nyangpuli compare -domain counsel -input fixtures/counsel.json
```

결과에는 두 조건의 근거와 `generation_inputs`가 들어 있습니다. 기본 `generation[].status`는 `not_run`입니다. 가상의 답변을 실제 모델 결과로 대체하지 않습니다.

사주·일운·궁합에는 Node와 pnpm이 추가로 필요합니다. **Go가 기존 TypeScript 계산 엔진을 호출하는 구조**이며 달력 알고리즘을 Go로 다시 쓴 것은 아닙니다. 타로는 공개 카드 의미 JSON을 Go에서 읽고 조립하며 원본 Python 앱 전체를 실행하는 방식은 아닙니다.

```bash
npm install -g pnpm@10.15.0
bash scripts/setup-saju.sh
go run ./cmd/nyangpuli compare -domain saju -input fixtures/saju.json
go run ./cmd/nyangpuli compare -domain daily -input fixtures/daily.json
go run ./cmd/nyangpuli compare -domain compatibility -input fixtures/compatibility.json
SAJU_INTEGRATION=1 go test ./internal/saju -count=1
node scripts/engine-regression.mjs
```

모든 명령은 저장소 루트에서 실행합니다. 엔진 소스·의존성은 고정 버전으로 설치하며 `.upstream/`은 커밋하지 않습니다. 시간 미상·음력 입력은 이 초기 API에서 지원하지 않으며 임의 시간을 넣지 않습니다.

## 로컬 API

```bash
export NYANG_API_TOKEN="$(go run ./cmd/nyangpuli token)"
export NYANG_USER_ID=local-demo
go run ./cmd/nyangpuli serve
```

`127.0.0.1:8080`에만 연결합니다. 다른 터미널에도 같은 토큰을 설정해야 합니다. `.env` 파일은 자동으로 읽지 않습니다. 샘플 환경변수는 `.env.example`에 있습니다.

```bash
curl http://127.0.0.1:8080/v1/compare/counsel \
  -H "Authorization: Bearer $NYANG_API_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"case_id":"C1","input":{"question":"연락 기준을 함께 정하고 싶어","topic":"negotiate_contact"},"use_memory":false}'
```

| 경로 | 역할 |
|---|---|
| `GET /healthz` | 공개 상태 확인 |
| `POST /v1/compare/{saju,daily,compatibility,tarot,counsel}` | `{case_id,input,use_memory}` 입력으로 근거 비교 |
| `POST /v1/readings` | `{domain,variant,input,use_memory}`와 `Idempotency-Key`로 요약 생성·저장 |
| `GET /v1/readings` | 인증된 사용자의 보관함 |
| `GET /v1/readings/{id}` | 저장한 요약·해제된 상세 조회 |
| `POST /v1/readings/{id}/detail` | 권한 확인 후 같은 입력 스냅샷으로 상세 생성·저장 |
| `POST /v1/dev/readings/{id}/unlock` | `--test-unlocks`를 켠 개발 서버에서만 상세 테스트 해제 |
| `POST /v1/memory` | `{key,value,source_event_id,observed_at}` 확정 사실 저장 |
| `GET /v1/memory` | 인증된 사용자의 현재 사실 |
| `GET /v1/memory/history?key=...` | 사실 변경 이력 |
| `DELETE /v1/memory` | 인증된 사용자의 로컬 사실·이력 삭제 |
| `POST /v1/memory/remote` | 선택적 Mem0에 `{messages,infer}` 전송 |
| `POST /v1/compare/memory` | 로컬 현재 사실과 선택적 Mem0 검색. `{query,use_mem0}` |

사용자는 요청 본문의 `user_id`로 바꿀 수 없습니다. 서버에서 토큰→사용자를 연결합니다. 여러 실험 사용자는 `NYANG_USERS_JSON='{"alice":"32자이상무작위토큰A","bob":"32자이상무작위토큰B"}'` 형식으로 구성합니다. 배포용 로그인 구현은 아닙니다.

모델을 설정하지 않은 기본 서버에서도 비교·기억·기존 보관함 조회는 가능합니다. 새 풀이 생성은 503을 반환하며 가상 답변으로 대체하지 않습니다. 풀이 스냅샷은 기본 `data/private/readings.json`에 저장하고 `serve --readings <path>`로 경로를 바꿉니다.

`use_memory:true`는 상담에서만 사용하며 두 조건에 동일한 기억을 제공합니다. 연락 경계·관계 상태, 최근 사건 순서로 최대 16개를 넣고 제외한 키를 `memory_selection`에 표시합니다. 핵심 연락 경계를 넣을 수 없으면 확인 없이 진행하지 않습니다. 현재 저장소는 단일 프로세스 JSON이며 암호화하지 않습니다. **실제 민감한 사연 수집 전에 별도 운영 DB·로그인·암호화·보관 정책이 필요합니다.**

Mem0는 `MEM0_BASE_URL`과 `MEM0_API_KEY`를 설정했을 때만 활성화됩니다. 외부 전송은 해당 원격 엔드포인트 또는 `use_mem0:true` 요청에 한정합니다. 로컬 사실은 Mem0에 자동 복사되지 않으며 한 저장소에서 삭제했다고 다른 저장소가 함께 삭제되지는 않습니다. 상세 계약과 원격 삭제 라이브러리 API는 [기억 문서](docs/memory.md)를 참고하세요.

## 실제 AI 답변 비교

`NYANG_LLM_API_BASE`, `NYANG_LLM_API_KEY`, `NYANG_LLM_MODEL`을 환경변수로 구성합니다. 공급자·모델은 직접 선택하며 키를 커밋하지 않습니다. 다음 명령은 실제 호출과 비용을 발생시킬 수 있습니다.

```bash
go run ./cmd/nyangpuli compare \
  -domain tarot -input fixtures/tarot.json -case tarot-001 \
  -generate -out outputs/tarot-001.json -blind-out outputs/tarot-001-blind
```

평가자에게는 `blind.json`과 `review.csv`를 제공합니다. 조건 이름을 숨긴 답변뿐 아니라 당시의 공통 상황·카드도 포함합니다. `answer-key.json`과 `raw-results.json`은 평가를 확정한 뒤 공개합니다. 자동 만점 평가나 임의 적중률은 만들지 않습니다.

제품 API의 요약→상세 흐름은 같은 공급자 설정으로 별도 실행합니다.

```bash
go run ./cmd/nyangpuli serve --enable-generation --test-unlocks
```

`--enable-generation`은 서버의 실제 모델 호출을 허용하므로 비용이 발생할 수 있습니다. `--test-unlocks`는 개발용 해제 경로만 열며 결제·영수증 검증을 하지 않습니다. 두 옵션은 기본 비활성입니다. 요약과 상세는 각각 별도 생성이며, 성공한 동일 요청이나 저장 결과의 재열람은 다시 생성하지 않습니다. 요청·응답 예시와 장애 시 재시도 한계는 [제품 흐름](docs/product-flow.md)에 있습니다. 요약은 유용한 답을 먼저 제공하고 상세는 같은 사실·카드·명식의 설명을 확장합니다. `baseline/enhanced`는 자료 조합이며 무료/유료 등급이 아닙니다.

## 검증과 다음 실험

```bash
go test -race ./...
go vet ./...
SAJU_INTEGRATION=1 go test ./internal/saju -count=1
node scripts/engine-regression.mjs
python3 scripts/validate.py
```

실행한 결과는 [검증 기록](docs/validation.md)에 있습니다. 전체 Go 시험 94개 pass 이벤트, 실제 원본 통합 재실행 19개 pass 이벤트, 고정 원본 회귀벡터 20/20 일치를 확인했습니다. 이벤트 수는 부모·하위 시험을 포함하며 서로 독립적인 사례 수로 합산하지 않습니다. 제품 API는 모의 모델로 요약·잠금·해제·상세·보관함·사용자 격리를 확인했습니다. 실제 LLM 답변, 실사용자·전문가 평가와 실제 미래 사건 결과는 아직 없습니다.

다음 실험은 같은 상황에서 두 조건을 생성하고, 미제공 사실·상대 마음 단정·거절 무시·낡은 기억 사용을 먼저 세는 것입니다. 공감·도움 평가는 별도로 기록합니다. 사건 예측을 시험하려면 결과가 나오기 전에 예측과 기간을 고정하고 실제 결과를 추적해야 합니다.

- [사주·일운](docs/saju.md)
- [타로](docs/tarot.md)
- [기억](docs/memory.md)
- [상담](docs/counsel.md)
- [비교·블라인드 평가](docs/evaluation.md)
- [Go 제품 API와 요약·상세 흐름](docs/product-flow.md)
- [원본 엔진 회귀시험과 Go 이식](docs/engine-migration.md)
- [외부 소스와 라이선스](NOTICE.md)

저장소에는 합성 예제만 포함합니다. 사용자 사연·API 키·실험 결과 출력 디렉터리는 커밋 대상에서 제외합니다. 새로 작성한 프로젝트 코드를 오픈소스로 공개하는 라이선스는 부여하지 않았으며, 포함된 외부 자료는 각각의 라이선스가 적용됩니다.
