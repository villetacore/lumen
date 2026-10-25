import { ApiError } from "../api/client";

/** isConflict reports a revision mismatch: the file changed since it was read. */
export function isConflict(err: unknown): boolean {
  return err instanceof ApiError && err.status === 409 && !/already exists/.test(err.message);
}

export function isNotFound(err: unknown): boolean {
  return err instanceof ApiError && err.status === 404;
}

/** errorText turns an API failure into a sentence for the person, not a status code. */
export function errorText(err: unknown): string {
  if (err instanceof ApiError) {
    if (err.status === 409 && /already exists/.test(err.message)) return "Файл с таким именем уже существует.";
    if (err.status === 409) return "Файл изменился, пока вы редактировали. Обновите и повторите.";
    if (err.status === 404) return "Не найдено — возможно, файл удалён или переименован.";
    if (err.status === 401 || err.status === 403) return "Нет доступа — войдите заново.";
    return err.message;
  }
  if (err instanceof TypeError) return "Сервер недоступен — проверьте, что lumend запущен.";
  return err instanceof Error ? err.message : String(err);
}
