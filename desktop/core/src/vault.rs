//! Работа с папкой vault: обход, чтение, атомарная запись.

use std::fs;
use std::io::{self, Write};
use std::path::{Path, PathBuf};
use walkdir::WalkDir;

pub struct Vault {
    pub root: PathBuf,
}

#[derive(Clone, Debug)]
pub struct FileInfo {
    pub path: String, // относительный путь со слэшами
    pub size: u64,
}

impl Vault {
    pub fn open(root: impl AsRef<Path>) -> io::Result<Vault> {
        let root = fs::canonicalize(root)?;
        if !root.is_dir() {
            return Err(io::Error::new(io::ErrorKind::InvalidInput, "vault root is not a directory"));
        }
        Ok(Vault { root })
    }

    /// Скрытые файлы и папки (включая `.lumen/`) в индекс не попадают.
    pub fn is_hidden(rel: &str) -> bool {
        rel.split('/').any(|p| !p.is_empty() && p.starts_with('.'))
    }

    pub fn abs(&self, rel: &str) -> io::Result<PathBuf> {
        let candidate = self.root.join(rel.trim_start_matches('/'));
        // Простая защита от выхода за корень; canonicalize требует существования файла.
        if rel.split('/').any(|p| p == "..") {
            return Err(io::Error::new(io::ErrorKind::InvalidInput, "path escapes the vault root"));
        }
        Ok(candidate)
    }

    /// Перечисляет все markdown-файлы vault.
    pub fn list(&self) -> Vec<FileInfo> {
        let mut out = Vec::new();
        for entry in WalkDir::new(&self.root).into_iter().filter_map(Result::ok) {
            if !entry.file_type().is_file() {
                continue;
            }
            let Ok(rel) = entry.path().strip_prefix(&self.root) else { continue };
            let rel = rel.to_string_lossy().replace('\\', "/");
            if Self::is_hidden(&rel) || !rel.to_lowercase().ends_with(".md") {
                continue;
            }
            let size = entry.metadata().map(|m| m.len()).unwrap_or(0);
            out.push(FileInfo { path: rel, size });
        }
        out.sort_by(|a, b| a.path.cmp(&b.path));
        out
    }

    pub fn read(&self, rel: &str) -> io::Result<Vec<u8>> {
        fs::read(self.abs(rel)?)
    }

    /// Атомарная запись: временный файл рядом, fsync, затем rename.
    pub fn write(&self, rel: &str, data: &[u8]) -> io::Result<()> {
        let abs = self.abs(rel)?;
        if let Some(dir) = abs.parent() {
            fs::create_dir_all(dir)?;
        }
        let tmp = abs.with_extension("lumen-tmp");
        {
            let mut f = fs::File::create(&tmp)?;
            f.write_all(data)?;
            f.sync_all()?;
        }
        fs::rename(&tmp, &abs)
    }
}
