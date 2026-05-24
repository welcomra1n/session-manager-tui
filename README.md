# session-manager-tui

Claude Code + Codex 세션을 통합 관리하는 터미널 UI. 세션 검색, 미리보기, 고정, 삭제, 내보내기, 휴지통, AI 요약을 하나의 TUI에서.

> [borball/claude-session-manager-tui](https://github.com/borball/claude-session-manager-tui) fork

## 스크린샷

```
┌ 세션 목록 ═══════════════════════════════════════════════════════════┐
│ Claude 🧠 (1)                                                        │
│   + 새 Claude 세션                                                   │
│   고정 📌 (1)                                                        │
│   └ ▶ 05/24 15:16  멀티 세션 매니저              CLI  D-29           │
│                                                                       │
│ Codex 🤖 (10)                                                        │
│   + 새 Codex 세션                                                    │
│   세션 (10)                                                           │
│     디자이너드 (3)                                                   │
│     ├   05/23 21:58  디자이너드 웹               DSK  D-29           │
│     ├   04/28 22:02  앱 출시 담당관              DSK  D-4            │
│     └   04/13 12:08  메일 정리                   DSK  D+11           │
│     Dropbox (3)                                                       │
│     ├   05/23 10:27  차량 관리 담당관            DSK  D-29           │
│     ├   05/23 10:10  재정 담당관                 DSK  D-29           │
│     └   04/15 14:44  파일정리 담당관             DSK  D+9            │
│     미분류 (4)                                                       │
│     ├   05/21 21:15  젠더리빌                    DSK  D-27           │
│     └   04/20 23:36  노래를 만들어 보리라        DSK  D+3            │
└───────────────────────────────────────────────────────────────────────┘
 Enter 열기 | p 미리보기 | t 고정 | s 정렬 | m 이름변경 | d 삭제 | e 내보내기
 Space 선택 | D 일괄삭제 | E 일괄내보내기 | c 컴팩트 | x 휴지통 | / 검색 | ? 도움말 | Esc 종료
          11개 세션 (🧠1 🤖10 · 245개 메시지 · Ghostty · 날짜순)
```

## 주요 기능

### 통합 세션 관리
- **Claude Code + Codex** 세션을 하나의 트리뷰에서 관리
- 🧠 Claude / 🤖 Codex 아이콘 구분
- `CLI` / `DSK` / `WEB` entrypoint 표시
- 프로젝트 폴더별 서브그룹 (접기/펼치기)

### 세션 구조
```
Provider (Claude/Codex)
├ + 새 세션
├ 고정 📌 — t키로 고정/해제, Codex 데스크탑 양방향 동기화
└ 세션
  ├ 프로젝트A (N)
  │ ├ 세션1
  │ └ 세션2
  ├ 프로젝트B (N)
  └ 미분류 (N)
```

### 세션 정보
- **D-day 만료 표시** — 30일 기준 (D-29, D+3 등)
- **활성 세션 감지** — ▶ 아이콘 (2분 이내 수정)
- **날짜 색상** — 파랑(2분 이내), 초록(7일), 회색(이후), 빨강(만료)
- **세션 통계** — 하단바에 provider별 수, 총 메시지 수, 활성 세션 수

### 세션 관리
- **리네임** (`m`) — 세션/프로젝트 별칭 저장. Claude Code `/rename` 자동 연동
- **삭제** (`d`) — 확인 모달 (d키로 확인 가능). 휴지통으로 이동
- **멀티 선택** (`Space`) — 여러 세션 선택 (✓ 표시)
- **일괄 삭제** (`D`) — 선택된 세션 한번에 삭제
- **일괄 내보내기** (`E`) — 선택된 세션 한번에 마크다운 내보내기
- **휴지통** (`x`) — 삭제된 세션 복원 또는 영구 삭제
- **내보내기** (`e`) — 대화 내용을 마크다운 파일로 바탕화면에 저장
- **고정** (`t`) — 세션 고정/해제. Codex 데스크탑과 양방향 동기화
- **정렬** (`s`) — 날짜순 / 이름순 / 만료순 토글
- **폴더 열기** (`o`) — Finder에서 세션 파일 위치 열기
- **미리보기** (`p`) — 세션 정보 + 대화 미리보기 패널
- **AI 요약** (`i`) — Claude Haiku로 세션 요약 생성
- **검색** (`/`) — 프로젝트명, 별칭, 메시지 검색 + 하이라이트
- **새 세션** — 프로젝트 폴더 선택 후 생성 (full-access 권한)

### 터미널 지원

| 터미널 | 감지 | 세션 열기 |
|--------|------|----------|
| Ghostty | `TERM_PROGRAM=ghostty` | 새 창 |
| iTerm2 | `TERM_PROGRAM=iTerm.app` | 새 탭 |
| Terminal.app | `TERM_PROGRAM=Apple_Terminal` | 새 탭 |
| tmux | `TMUX` 환경변수 (세션 안) | 새 창 |
| Kitty | `KITTY_PID` 환경변수 | 새 탭 |
| WezTerm | `TERM_PROGRAM=WezTerm` | 새 탭 |
| Fallback | 기타 | TUI 일시중지 후 실행 |

### 컴팩트 모드
- **컴팩트 토글** (`c`) — 좁은 화면용 축약 레이아웃
- 활성 아이콘 + 제목 22칸 + D-day만 표시
- 핸드폰 SSH 접속 시 최적화

### 자동 업데이트 알림
- 앱 시작 시 GitHub releases 자동 체크
- 새 버전 있으면 하단 상태바에 `⬆ 새 버전 X.X.X 사용 가능` 표시

### 기타
- **한글 키보드 지원** — 두벌식 한글 상태에서도 단축키 동작 (자모 + 완성형 분해)
- **10초 자동 갱신** — 세션 변경 실시간 반영
- **전체 UI 한글화**
- **도움말** (`?`) — 색상, 아이콘, 단축키 설명
- **배경 완전 블랙** (#000000)

## 설치

Go 1.21+ 필요.

```bash
git clone https://github.com/welcomra1n/session-manager-tui.git
cd session-manager-tui
go build -o csm .
```

PATH에 추가:

```bash
cp csm /usr/local/bin/
```

## 사용법

```bash
csm
```

## 단축키

| 키 | 기능 |
|----|------|
| `Enter` | 세션 열기 / 그룹 접기·펼치기 |
| `p` | 미리보기 패널 토글 |
| `t` | 세션 고정/해제 |
| `s` | 정렬 토글 (날짜/이름/만료순) |
| `m` | 세션 또는 프로젝트 이름 변경 |
| `Space` | 멀티 선택 (✓) |
| `d` | 삭제 (휴지통 이동, d로 확인) |
| `D` | 선택된 세션 일괄 삭제 |
| `e` | 마크다운 내보내기 |
| `E` | 선택된 세션 일괄 내보내기 |
| `c` | 컴팩트 모드 토글 |
| `x` | 휴지통 보기 (복원/영구삭제) |
| `o` | 세션 폴더 Finder에서 열기 |
| `i` | AI 요약 생성 |
| `/` | 검색 (하이라이트) |
| `r` | 새로고침 |
| `←` `→` | 그룹 접기/펼치기 |
| `?` | 도움말 |
| `Esc` | 종료 |

## 데이터 저장 위치

| 파일 | 용도 |
|------|------|
| `~/.claude/session-aliases.json` | 세션 별칭 |
| `~/.claude/project-aliases.json` | 프로젝트 별칭 |
| `~/.claude/session-pins.json` | 고정 세션 목록 |
| `~/.claude/session-unpins.json` | 고정 해제 목록 |
| `~/.claude/session-trash/` | 휴지통 (삭제된 세션) |

## 원격 접속 (Tailscale / SSH)

핸드폰이나 다른 기기에서 세션 관리 가능:

```bash
# Tailscale 네트워크를 통해 Mac에 SSH 접속
ssh user@your-mac.tailnet-name.ts.net

# csm 실행
csm
```

추천 모바일 SSH 앱: **Termius**, **Blink Shell**, **a-Shell**

## 크로스 플랫폼 빌드

```bash
make build-all
```

`dist/` 폴더에 생성:
- `csm-darwin-arm64` (Apple Silicon Mac)
- `csm-darwin-amd64` (Intel Mac)
- `csm-linux-amd64`
- `csm-linux-arm64`
- `csm-windows-amd64.exe`

## Homebrew 설치 (예정)

```bash
brew tap welcomra1n/tap
brew install csm
```

## 개선 예정

- [ ] 세션 태그 — #작업, #개인 등 태그로 분류
- [ ] 세션 메모 — 각 세션에 한 줄 메모 추가
- [ ] 테마 설정 — `~/.claude/csm-theme.json`으로 색상 커스텀
- [ ] 설정 파일 — `~/.claude/csm-config.toml` (정렬 기본값, 만료 기준일 등)
- [ ] Gemini CLI 지원 — 3번째 provider
- [ ] 세션 복제 — 기존 세션 fork해서 새 세션
- [x] 자동 업데이트 알림
- [x] 멀티 선택 일괄 삭제/내보내기
- [x] 컴팩트 모드 (모바일 최적화)

## 라이선스

MIT
