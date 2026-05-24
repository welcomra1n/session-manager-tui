# session-manager-tui

Claude Code + Codex 세션을 통합 관리하는 터미널 UI. 세션 검색, 미리보기, 고정, 삭제, AI 요약, 새 세션 생성을 하나의 TUI에서.

> [borball/claude-session-manager-tui](https://github.com/borball/claude-session-manager-tui) fork

## 스크린샷

```
┌ 세션 목록 ═══════════════════════════════════════════════════════════════┐
│── Claude 🧠 (5) ──                                                       │
│   + 새 Claude 세션                                                       │
│   ▼ 고정 📌 (1)                                                          │
│   └ ▶ 05/24 09:15  welcomra1n         멀티 세션 매니저       CLI  D-29   │
│   ▼ 세션 (4)                                                             │
│   ├   05/22 19:30  Keynimal           아이폰 기본 키보드     DSK  D-29   │
│   ├   05/19 19:51  welcomra1n         하네스 에르메스?       CLI  D-26   │
│   └   05/03 23:50  DearCal            다른사람들이 사용할…   DSK  D-10   │
│                                                                           │
│── Codex 🤖 (12) ──                                                       │
│   + 새 Codex 세션                                                        │
│   ▶ 고정 📌 (1)                                                          │
│   ▼ 세션 (11)                                                            │
│   ├   05/23 21:58  _Deisgnerd(2022)   디자이너드 웹          DSK  D-29   │
│   ├   05/23 10:27  Dropbox            차량 관리 담당관       DSK  D-29   │
│   └   04/13 12:08  _Deisgnerd(2022)   메일 정리              DSK  D+10  │
└───────────────────────────────────────────────────────────────────────────┘
 Enter 열기 | p 미리보기 | t 고정 | Space 선택 | m 이름변경 | d 삭제 | D 일괄삭제
 o 폴더 | / 검색 | r 새로고침 | ? 도움말 | Esc 종료
                              13개 세션 (Ghostty)
```

## 주요 기능

### 통합 세션 관리
- **Claude Code + Codex** 세션을 하나의 목록에서 관리
- 🧠 Claude / 🤖 Codex 아이콘으로 구분
- `CLI` / `DSK` / `WEB` entrypoint 표시

### 세션 구조
- **새 세션** — 그룹 상단에서 바로 생성
- **고정 세션 📌** — `t`키로 고정/해제, Codex 데스크탑 고정 자동 반영
- **일반 세션** — 날짜순 정렬
- **←/→ 방향키** — 고정/일반 그룹 접기/펼치기

### 세션 정보
- **D-day 만료 표시** — 30일 기준 (D-29, D+3 등)
- **활성 세션 감지** — ▶ 아이콘 (2분 이내 수정)
- **날짜 색상** — 파랑(2분), 초록(7일), 회색(이후), 빨강(만료)
- **프로젝트 폴더명** — Codex는 session_meta CWD에서 읽음

### 세션 관리
- **리네임** (`m`) — 별칭 저장. Claude Code `/rename` 자동 연동
- **삭제** (`d`) — 단일 삭제 (확인 모달)
- **일괄 삭제** (`Space` 선택 → `D`) — 다중 삭제
- **폴더 열기** (`o`) — Finder에서 세션 파일 위치 열기
- **미리보기** (`p`) — 세션 정보 + 대화 미리보기 패널
- **AI 요약** (`i`) — Claude Haiku로 세션 요약 생성
- **검색** (`/`) — 프로젝트명, 별칭, 메시지 검색

### 터미널 지원

| 터미널 | 감지 | 세션 열기 |
|--------|------|----------|
| Ghostty | `TERM_PROGRAM=ghostty` | 새 창 |
| iTerm2 | `TERM_PROGRAM=iTerm.app` | 새 탭 |
| Terminal.app | `TERM_PROGRAM=Apple_Terminal` | 새 탭 |
| tmux | `TMUX` 환경변수 | 새 창 |
| Kitty | `KITTY_PID` 환경변수 | 새 탭 |
| WezTerm | `TERM_PROGRAM=WezTerm` | 새 탭 |
| Fallback | 기타 | TUI 일시중지 후 실행 |

### 기타
- **한글 키보드 지원** — 두벌식 한글 상태에서도 단축키 동작
- **10초 자동 갱신** — 세션 변경 실시간 반영
- **전체 UI 한글화**
- **도움말** (`?`) — 색상, 아이콘, 단축키 설명

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
| `Enter` | 세션 열기 (새 터미널 창) |
| `p` | 미리보기 패널 토글 |
| `t` | 세션 고정/해제 |
| `Space` | 다중 선택 |
| `m` | 세션 이름 변경 |
| `d` | 선택 세션 삭제 |
| `D` | 선택된 세션 일괄 삭제 |
| `o` | 세션 폴더 Finder에서 열기 |
| `i` | AI 요약 생성 |
| `/` | 검색 |
| `r` | 새로고침 |
| `←` | 그룹 접기 |
| `→` | 그룹 펼치기 |
| `?` | 도움말 |
| `Esc` | 종료 |

## 개선 예정

- [ ] `tview.TreeView` 전환 — 진짜 트리 구조로 더 자연스러운 접기/펼치기
- [ ] 프로젝트별 그룹핑 — Codex 데스크탑처럼 프로젝트 폴더별 세션 묶기
- [ ] 세션 정렬 옵션 — 이름순/날짜순/만료순 토글
- [ ] 만료 임박 알림 — D-3 이하 세션 상단 경고 배너
- [ ] Windows 지원 — Windows Terminal 백엔드
- [ ] 세션 내보내기 — 대화 내용 마크다운 파일로 저장
- [ ] 세션 통계 — 총 토큰 사용량, 세션별 메시지 수 차트
- [ ] 테마 커스터마이징 — 색상/아이콘 설정 파일

## 라이선스

MIT
