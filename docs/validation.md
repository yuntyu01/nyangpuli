# 실제 실행한 검증

2026-10-30 UTC 기록. 원시 요약은 [validation-results.json](validation-results.json)에 있다(`python3 scripts/validate.py`가 덮어쓴다). 환경: Rocky Linux 9(WSL2), Go 1.26.5, Node 22.21.0(모바일 단위 테스트와 회귀 기록), PostgreSQL 17.6 컨테이너, pnpm 10.20.0. CI는 Go 1.27.3·Node 24를 쓰며 이번 기록에는 원격 GitHub Actions 결과가 포함되지 않는다.

## 자동 검사

| 검사 | 결과 |
|---|---|
| `go vet ./...` | 통과 |
| `SAJU_INTEGRATION=1 NYANG_TEST_DATABASE_URL=… go test -race -count=1 ./...` | 통과. pass 이벤트 207개(최상위 154, 하위 53), skip 0, fail 0. 실제 PostgreSQL과 고정 OhMySaju 빌드를 사용 |
| `python3 scripts/validate.py`(DB 설정, 사주 통합 없이 전체 + 사주 패키지 별도) | 통과. 전체 실행 pass 201개(사주 원본 통합 시험 6개 skip), 사주 통합 실행 27개 pass |
| `node scripts/engine-regression.mjs` | 20/20 일치, 출처 검사 통과(원인 조사는 [엔진 이식 문서](engine-migration.md)) |
| CLI 비교 5개 영역 | 각각 두 조건 생성, 모델 상태 `not_run` |
| `conversation-eval` 모의 없는 준비 실행 | 가상 대화 12개 문맥 검사 12/12 통과, 입력 24개 준비, 모델 호출 0 |
| 컴파일 바이너리 HTTP(JSON 랩 모드) | 상태 확인·인증된 상담 비교·정상 종료 |
| 모바일 `npm test` | 53/53 통과(Node 22.21.0). Node 20에서는 `.ts` 직접 실행이 안 되어 12개 파일이 실패로 보고된다. 코드 문제가 아니라 실행 환경 조건 |
| 모바일 `tsc --noEmit` | 통과 |
| 모바일 `expo export --platform android` | JS 번들 생성 통과 |

이벤트 수는 부모·하위 시험을 함께 센 값이며 서로 독립적인 사례 수가 아니다.

## 수동 통합 확인 (모의 모델)

컴파일한 서버를 새 PostgreSQL DB와 로컬 모의 모델 서버(파이썬 HTTP, 고정 JSON 응답)에 연결해 HTTP로 실행했다.

- 게스트 계정 2개 생성, 타로 세션 생성·선택: 카드 설명이 `tarot-product-v1`, 출처가 Arcanite 파일.
- 타로 무료 요약 생성: `arcanite_product_v1`, 출처에 `policy:tarot-product-v1`.
- 다른 계정이 그 풀이를 대화에 연결하거나 대화를 읽으면 404.
- 풀이를 연결한 대화 생성(`tarot`, `self`), 메시지 전송 202, 같은 요청 키 재전송은 `replayed:true`로 같은 차례 반환.
- 답변 `complete`, 출력 검사 7개 모두 pass, 사용자 발화 인용이 있는 기억 1개가 `episode`·`user_stated`로 저장.

모의 모델은 형식과 흐름만 확인한다. 답변 품질을 뜻하지 않는다.

## 이번 변경으로 추가된 시험

- 타로 카드 설명·요약·상세가 같은 Arcanite 근거를 쓰는지, 정책 이전 `enhanced` 풀이를 같은 카드로 재사용하는지(`internal/api/tarot_reading_test.go`).
- 제품 정책이 Tarotoo와 YueYuan을 섞지 않는지(`internal/lab/policy_test.go`, 사주는 원본 엔진 통합 시험).
- 대화 문맥 구성, 출력 검사, 실제 DB 대화 흐름(`internal/conversation`), HTTP 경로(`internal/api/conversations_test.go`), 참고 자료 조건(`internal/counsel/conversation_test.go`), 평가 준비와 검토지(`internal/evaluation/conversation_test.go`, `cmd/conversation-eval`).
- 모바일 대화 상태 검증, 재전송 키 유지, 결과 연결 대상 선택, 기억 표시(`apps/mobile/tests/conversation.test.mjs`, `memory.test.mjs`).

## 실행하지 못한 검증

| 항목 | 이유 |
|---|---|
| 실제 LLM 답변 생성·비교 | 모델 API 키 없음. 실제 호출 0회 |
| 가상 대화의 블라인드 사람 평가 | 실제 답변이 없어 진행 불가. 평가자 0명 |
| 실제 Mem0 서버 | 계정·서버 없음. 0회 |
| Android/iOS 실기기·에뮬레이터 화면 | 이번 작업에서 기기 연결과 화면 확인을 하지 않음 |
| 스토어 결제·샌드박스 | 스토어 계정 없음 |
| KASI 공식 API 실호출 | 인증키 없음 |
| 원격 GitHub Actions 결과 | 푸시 후 결과를 이 기록에 포함하지 않음 |

가상 대화와 모의 모델 결과를 실제 사용자 효과, 상담 품질, 미래 적중률로 해석하지 않는다.
