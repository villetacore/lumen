//! Общие с сервером golden-фикстуры (ADR-0004). Файлы лежат в spec/testdata и
//! обязаны разбираться обеими реализациями одинаково.

use lumen_core::md::{apply_task_patch, TaskPatch};
use lumen_core::{parse_note, TaskStatus};
use std::fs;
use std::path::PathBuf;

fn fixtures_dir() -> PathBuf {
    PathBuf::from(env!("CARGO_MANIFEST_DIR")).join("../../spec/testdata")
}

#[test]
fn fixtures_parse_and_round_trip() {
    let dir = fixtures_dir();
    let entries = fs::read_dir(&dir).expect("spec/testdata must exist");
    let mut seen = 0;

    for entry in entries.flatten() {
        let path = entry.path();
        if path.extension().and_then(|e| e.to_str()) != Some("md") {
            continue;
        }
        let name = path.file_name().unwrap().to_string_lossy().to_string();
        let data = fs::read(&path).unwrap();
        let note = parse_note(&name, &data);
        seen += 1;

        // Свойство lossless: пустой патч возвращает исходную строку байт в байт.
        for task in &note.tasks {
            let out = apply_task_patch(&task.raw, &TaskPatch::default())
                .unwrap_or_else(|| panic!("{name}:{} is not a task line", task.line));
            assert_eq!(out, task.raw, "{name}:{} lossless round-trip failed", task.line);
        }
    }
    assert!(seen > 0, "no fixtures found in {}", dir.display());
}

#[test]
fn task_statuses_and_fields() {
    let src = concat!(
        "- [ ] Простая\n",
        "- [x] Готово [done:: 2026-09-08] ^f0a1\n",
        "- [/] В работе [est:: 2h30m] ^f0a2\n",
        "- [-] Отменено\n",
        "- [ ] Всё сразу [due:: 2026-09-10] [prio:: A] [project:: [[Запуск Lumen]]] #work ^f0a4\n",
    );
    let note = parse_note("t.md", src.as_bytes());
    assert_eq!(note.tasks.len(), 5);
    assert_eq!(note.tasks[0].status, TaskStatus::Todo);
    assert_eq!(note.tasks[1].status, TaskStatus::Done);
    assert_eq!(note.tasks[2].status, TaskStatus::Doing);
    assert_eq!(note.tasks[3].status, TaskStatus::Cancelled);

    let full = &note.tasks[4];
    assert_eq!(full.text, "Всё сразу #work");
    assert_eq!(full.due.unwrap().to_string(), "2026-09-10");
    assert_eq!(full.project.as_deref(), Some("Запуск Lumen"));
    assert_eq!(full.block_id.as_deref(), Some("f0a4"));
    assert_eq!(full.tags, vec!["work".to_string()]);
}

#[test]
fn fenced_code_is_not_indexed() {
    let src = "# Заметка\n\n```\n- [ ] не задача #нетег\n[[не ссылка]]\n```\n\n- [ ] задача ^n0a1\n";
    let note = parse_note("t.md", src.as_bytes());
    assert_eq!(note.tasks.len(), 1);
    assert!(note.links.is_empty());
    assert!(!note.tags.iter().any(|t| t == "нетег"));
}

#[test]
fn patch_changes_only_what_it_names() {
    let line = "  - [ ] Купить молоко #дом [due:: 2026-09-10] ^m1a2b4";
    let patch = TaskPatch {
        status: Some(TaskStatus::Done),
        set: [("done".to_string(), "2026-09-08".to_string())].into_iter().collect(),
        ..Default::default()
    };
    let out = apply_task_patch(line, &patch).unwrap();
    assert_eq!(out, "  - [x] Купить молоко #дом [due:: 2026-09-10] [done:: 2026-09-08] ^m1a2b4");
}

#[test]
fn patch_appends_in_canonical_order() {
    let patch = TaskPatch {
        set: [
            ("prio".to_string(), "B".to_string()),
            ("due".to_string(), "2026-09-09".to_string()),
        ]
        .into_iter()
        .collect(),
        block_id: Some("4fuxbusg".to_string()),
        ..Default::default()
    };
    let out = apply_task_patch("- [ ] Прочитать статью", &patch).unwrap();
    assert_eq!(out, "- [ ] Прочитать статью [due:: 2026-09-09] [prio:: B] ^4fuxbusg");
}

#[test]
fn frontmatter_and_links() {
    let src = concat!(
        "---\n",
        "id: 01K5Q9V3JX8ZQ2N6C1B7M4TDA1\n",
        "title: Фикстура\n",
        "type: project\n",
        "tags: [spec, work]\n",
        "---\n",
        "\n",
        "Ссылка на [[Другая заметка|алиас]] и тег #проект/lumen.\n",
    );
    let note = parse_note("20-projects/x.md", src.as_bytes());
    assert_eq!(note.id, "01K5Q9V3JX8ZQ2N6C1B7M4TDA1");
    assert_eq!(note.title, "Фикстура");
    assert_eq!(note.links.len(), 1);
    assert_eq!(note.links[0].target, "Другая заметка");
    assert_eq!(note.links[0].alias, "алиас");
    assert!(note.tags.contains(&"проект/lumen".to_string()));
}
