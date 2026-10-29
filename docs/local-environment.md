# 로컬 개발 환경

## Compose

Docker Engine과 Compose v2가 필요하다. 루트에서 테스트 전용 비밀번호와 임의의 32바이트 이상 API 토큰을 환경변수로 넣는다. 아래의 비밀번호는 개발용 예시이며 실제 계정에 재사용하지 않는다. 환경변수 값은 로그나 소스에 기록하지 않는다.

```sh
export POSTGRES_PASSWORD=localdevelopmentpassword
export NYANG_API_TOKEN="$(openssl rand -hex 32)"
export NYANG_USER_ID=local-demo
docker compose up --build -d
curl --fail http://127.0.0.1:8080/healthz
docker compose logs api
```

관리형 클라우드에서 빌드할 때는 프록시 CA가 필요하다. 이 CA는 BuildKit 비밀 마운트로만 사용하며 이미지에 복사하지 않는다. 보통 환경에서는 기본 compose 파일만 사용한다.

```sh
# 홈 디렉터리가 읽기 전용인 관리형 환경에서 buildx 상태를 저장할 위치
export BUILDX_CONFIG=/tmp/nyang-buildx
docker compose -f compose.yaml -f compose.proxy.yaml up --build -d
```

이미지를 다시 빌드할 필요가 없다면 같은 환경변수와 Compose 파일을 사용해 `docker compose -f compose.yaml -f compose.proxy.yaml up -d`로 다시 시작한다. 관리형 환경에서 빌드와 재시작 후 상태 확인, 인증 거부 및 사주·타로·상담 비교 요청을 검증했다. 이는 실제 모델, 결제 또는 PostgreSQL 기반 전체 제품 흐름 검증을 뜻하지 않는다.

API와 DB의 호스트 포트는 로컬 인터페이스에만 노출한다. 컨테이너 내 API의 비루프백 바인딩은 `NYANG_ENV=development`와 명시적 `--allow-container-bind`가 모두 있을 때만 가능하다. 이 구성은 개발용이며 운영 공개에 적합하지 않다. 종료는 `docker compose down`으로 한다. 명명된 볼륨은 유지된다. `down -v`는 데이터를 삭제하므로 필요한 경우에만 사용한다.

현재 Compose는 PostgreSQL 서버를 준비하지만 랩 API의 기본 저장소는 여전히 로컬 JSON이다. DB 마이그레이션·세션 인증과 앱의 계정 발급 및 나머지 저장소를 연결하기 전에는 PostgreSQL 전환이 완료된 것이 아니다. `api-data` 볼륨에는 랩 데이터가 저장되므로 실제 민감 정보를 넣지 않는다.

## 백업과 복구

DB 사용 기능을 연결하고 검증한 뒤에만 PostgreSQL 백업을 사용한다. `docker compose exec -T db pg_dump -U nyangpuli -d nyangpuli -Fc > backup.dump`로 백업하고, 별도 빈 DB에 `pg_restore`로 복구한 다음 애플리케이션 검증을 한다. 덤프에는 개인정보가 포함될 수 있으므로 암호화된 접근 제한 저장소를 사용하고 Git에 추가하지 않는다. 현재 JSON 랩 데이터는 이 덤프에 포함되지 않는다.

## 외부 서비스

모델 호출은 별도 설정과 명시적 활성화가 필요하며 비용이 발생할 수 있다. 스토어 구매와 R2는 기본 구성에 필요하지 않다. 실제 키는 소스, 이미지, CI에 넣지 않는다. 사용 가능한 외부 계정과 실제 연동 상태는 `docs/implementation-status.md`를 확인한다.

## PostgreSQL 앱 모드

개발 계정, 프로필, 저장된 타로 세션, 개인 기억과 풀이를 실제 PostgreSQL로 사용하려면 기본 Compose에 `compose.postgres.yaml`을 더한다. 기존 JSON 데이터는 변환하거나 삭제하지 않는다. 두 모드의 데이터는 별개이며 JSON 이관 도구는 아직 없다. URL에 넣기 안전한 16진 개발 암호를 사용하고 운영 데이터나 비밀을 사용하지 않는다. 아래 암호 생성은 새 볼륨을 처음 만들 때 한 번만 실행한다. 기존 PostgreSQL 볼륨을 다시 시작할 때는 처음 설정한 비밀번호를 그대로 사용한다. 비밀번호를 바꾼 환경변수만으로 기존 DB 비밀번호는 변경되지 않는다.

```sh
export POSTGRES_PASSWORD="$(openssl rand -hex 24)"
export NYANG_API_TOKEN="$(openssl rand -hex 32)"
export BUILDX_CONFIG=/tmp/nyang-buildx
# 관리형 클라우드의 프록시 CA를 사용하는 빌드
docker compose -f compose.yaml -f compose.proxy.yaml -f compose.postgres.yaml up --build -d
curl --fail http://127.0.0.1:8080/healthz
```

이 모드의 계정 생성·로그인 계약은 `docs/accounts.md`를 참조한다. API 기본값은 실제 모델 생성과 테스트 구매를 켜지 않는다. 각 통합은 별도로 명시적 설정이 필요하며, 운영 모드는 차단된다. 포트 바인딩은 로컬 전용이고 실제 기기에서는 적절한 TLS 개발 연결이 필요하다.
