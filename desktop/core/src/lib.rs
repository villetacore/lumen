//! `lumen-core` — ядро десктопного клиента Lumen.
//!
//! Здесь живёт вся логика, которую UI на Flutter только показывает: разбор и
//! запись формата (spec/FORMAT.md), обход vault, индекс и запросы. Обвязка
//! `flutter_rust_bridge` появится в `src/api.rs` на этапе M2 — она будет тонким
//! слоем над этими типами, чтобы то же ядро можно было переиспользовать в CLI
//! и мобильном клиенте.

pub mod index;
pub mod md;
pub mod model;
pub mod vault;

pub use index::{resolve_view, Index, TaskQuery};
pub use md::{apply_task_patch, build_task, events, parse_note, TaskPatch};
pub use model::{Event, LDate, Note, NoteType, Priority, Task, TaskStatus};
pub use vault::Vault;

/// Рабочее пространство: vault на диске плюс его индекс.
pub struct Workspace {
    pub vault: Vault,
    pub index: Index,
}

impl Workspace {
    pub fn open(root: &str) -> std::io::Result<Workspace> {
        let vault = Vault::open(root)?;
        let mut index = Index::new();
        index.rebuild(&vault);
        Ok(Workspace { vault, index })
    }

    /// Перечитывает один файл после изменения на диске.
    pub fn reindex_path(&mut self, rel: &str) {
        match self.vault.read(rel) {
            Ok(data) => self.index.put(parse_note(rel, &data)),
            Err(_) => self.index.remove(rel),
        }
    }

    /// Применяет патч к задаче и переписывает только её строку в файле.
    pub fn patch_task(&mut self, id: &str, patch: &TaskPatch) -> std::io::Result<()> {
        let Some(task) = self.index.task(id) else {
            return Err(std::io::Error::new(std::io::ErrorKind::NotFound, "task not found"));
        };
        let (path, line_no) = (task.note_path.clone(), task.line);

        let data = self.vault.read(&path)?;
        let src = String::from_utf8_lossy(&data).to_string();
        let eol = if src.contains("\r\n") { "\r\n" } else { "\n" };
        let mut lines: Vec<String> =
            src.replace("\r\n", "\n").split('\n').map(str::to_string).collect();

        if line_no >= lines.len() {
            return Err(std::io::Error::new(std::io::ErrorKind::InvalidData, "task line is gone"));
        }
        let Some(updated) = apply_task_patch(&lines[line_no], patch) else {
            return Err(std::io::Error::new(std::io::ErrorKind::InvalidData, "not a task line"));
        };
        lines[line_no] = updated;

        self.vault.write(&path, lines.join(eol).as_bytes())?;
        self.reindex_path(&path);
        Ok(())
    }
}
