# 냥풀이 모바일 (초기 개발 화면)

Expo SDK 58, React Native 0.88, TypeScript 6을 사용합니다. SDK 58 공식 `expo-template-blank-typescript`의 React/React Native 버전과 Expo 패키지에 포함된 호환 버전을 기준으로 고정했습니다. `package-lock.json`으로 설치합니다. Node 24를 사용하세요.

```sh
cd apps/mobile
npm ci
npm test
npm run typecheck
# 앱에서 접근 가능한 백엔드 HTTPS 주소를 지정합니다. 비밀값을 여기에 넣지 마세요.
EXPO_PUBLIC_API_URL=https://your-development-api.example npm run android
# macOS + Xcode에서:
EXPO_PUBLIC_API_URL=https://your-development-api.example npm run ios
# 개발 빌드를 설치한 뒤 Metro 실행:
EXPO_PUBLIC_API_URL=https://your-development-api.example npm start
```

`expo run:android`에는 Android SDK와 에뮬레이터 또는 기기가 필요하고 `expo run:ios`에는 macOS/Xcode가 필요합니다. 개발 빌드를 사용하며 Expo Go만으로 검증했다고 주장하지 않습니다. 기기에서 `localhost`는 서버가 아니라 기기 자신입니다. TLS로 보호된 개발 서버를 사용하세요. 현재 설정 화면의 토큰 입력은 개발용 연구 서버와의 임시 연결 수단이며, 앱에 내장하거나 공개 환경변수에 넣지 않습니다. 토큰은 Expo SecureStore에 저장하고 로그에 출력하지 않습니다. 앱은 서버에서 검증한 계정만 사용하며 사용자 ID를 보내지 않습니다.

홈은 제공된 이미지의 라일락·핑크 카드, 하단 내비게이션과 글자 배치를 참고했습니다. 제공 이미지의 고양이 그래픽은 별도 이미지 파일이 없어 임시 이모지로 표시합니다. 화면 전체를 이미지로 사용하지 않습니다. 출생 정보는 메모리에만 보관되며 앱 종료 후 다시 입력해야 합니다. 오늘 날짜는 입력한 시간대로 계산합니다. 정확한 출생시간이 없거나 음력인 경우 현재 서버 엔진의 제한으로 결과를 요청하지 않습니다.

기존 API에 연결된 기능은 사주·오늘의 운세·궁합·상담 요약 요청, 보관함 조회, 상세 권한 확인, 기억 조회 및 전체 삭제입니다. 모델 미설정 시 결과를 꾸며내지 않고 오류를 표시합니다. 응답이 불명확한 요청은 자동 재생성하지 않고 보관함 확인을 안내합니다. 구매, 서버 카드 선택, 개별 기억 편집/삭제, 복구 가능한 계정 연결 및 계정 삭제는 아직 구현되지 않았습니다. 이 화면은 운영용 계정·결제 기능으로 배포할 수 없습니다.

검증: Node 단위 테스트 6건(윤일·입력 제한·시간대 날짜 경계·서버 URL 및 오류 처리), `tsc --noEmit`, Android용 Metro 번들 생성과 임시 디렉터리에서 Android 네이티브 프로젝트 생성(prebuild)이 통과했습니다. SDK 패키지 버전은 Expo의 로컬 호환 목록으로 검사했습니다(오프라인 검사). Android/iOS 네이티브 빌드와 실기기 화면·접근성·구매 흐름은 미검증입니다. 빌드 및 실제 기기 시험 후 긴 한국어, 글자 확대, 키보드, 작은 화면, 스크린 리더를 다시 확인해야 합니다.

사용자 연결을 변경하거나 해제하면 앱 메모리의 출생 정보, 질문, 기억 및 결과도 비웁니다. 보관함·기억 조회 실패는 빈 결과로 표시하지 않으며 다시 불러올 수 있습니다. 운영 빌드에서는 HTTPS 서버만 허용합니다. 앱의 실제 화면 접근성과 안전 영역은 기기에서 추가 검증해야 합니다.
