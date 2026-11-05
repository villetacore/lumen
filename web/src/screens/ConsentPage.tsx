"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import { Alert, Button, Spinner } from "@villetacore/velessoft-ui";
import { api } from "../api/client";
import { errorText } from "../lib/errors";

const FIELDS = ["client_id", "redirect_uri", "state", "response_type", "code_challenge", "code_challenge_method", "resource", "scope"] as const;

/**
 * ConsentPage is where a person allows an AI client to connect. Anyone can
 * register a client under any name, so the screen also says where the answer
 * goes. The server checks the request again when the answer arrives.
 */
export default function ConsentPage({ go = (url) => window.location.assign(url) }: { go?: (url: string) => void }) {
  const params = useMemo(() => {
    const q = new URLSearchParams(window.location.search);
    return Object.fromEntries(FIELDS.map((f) => [f, q.get(f) ?? ""])) as Record<(typeof FIELDS)[number], string>;
  }, []);
  const [leaving, setLeaving] = useState(false);
  const me = useQuery({ queryKey: ["me"], queryFn: api.me });
  const info = useQuery({ queryKey: ["consent", params.client_id], queryFn: () => api.consentInfo(params.client_id, params.redirect_uri), retry: false });
  const answer = useMutation({
    mutationFn: (approve: boolean) => api.consent({ ...params, approve }),
    onSuccess: ({ redirect_to }) => { setLeaving(true); go(redirect_to); },
  });

  return <div className="page">
    <div className="consent" aria-labelledby="consent-title" role="group">
      {info.isLoading && <Spinner label="Загрузка" />}
      {info.error && <>
        <h1 id="consent-title">Не удалось подключить клиент</h1>
        <Alert tone="error" title="Запрос отклонён">{errorText(info.error)}</Alert>
      </>}
      {leaving && <>
        <h1 id="consent-title">Готово</h1>
        <p>Возвращаемся в клиент. Эту вкладку можно закрыть.</p>
      </>}
      {info.data && !leaving && <>
        <h1 id="consent-title">Разрешить подключение?</h1>
        <p><strong>{info.data.client_name}</strong> просит доступ к Lumen{me.data?.name ? <> от имени <strong>{me.data.name}</strong></> : null}.</p>
        <p className="sub">Название клиент указывает сам. Ответ получит: <strong>{info.data.redirect_host}</strong>. Если вы не начинали подключение сейчас, откажите.</p>
        <ul className="consent-scope">
          <li>Читать заметки, документы и тикеты, которые видны вам.</li>
          <li>Предлагать изменения. Они применятся только после вашего подтверждения в Lumen.</li>
        </ul>
        <p className="sub">Нет доступа к паролям, интеграциям и администрированию. Подключение можно отключить на странице «AI-клиенты», через 30 дней его нужно будет разрешить заново.</p>
        {answer.error && <Alert tone="error" title="Не выполнено">{errorText(answer.error)}</Alert>}
        <div className="consent-actions">
          <Button variant="primary" disabled={answer.isPending} onClick={() => answer.mutate(true)}>Разрешить</Button>
          <Button disabled={answer.isPending} onClick={() => answer.mutate(false)}>Отказать</Button>
        </div>
      </>}
    </div>
  </div>;
}
