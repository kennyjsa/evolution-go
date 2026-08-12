import { useEffect, useRef, useState } from "react";
import { useNavigate, useParams } from "react-router-dom";
import { ArrowLeft, Copy, Save, Trash2 } from "lucide-react";
import { Button } from "@evoapi/design-system";
import { toast } from "sonner";
import { useForm } from "react-hook-form";
import { zodResolver } from "@hookform/resolvers/zod";
import { z } from "zod";
import * as chatwootApi from "@/services/api/chatwoot";
import * as instancesApi from "@/services/api/instances";
import type { Instance } from "@/types/instance";

// accountToken fica fora do required: salvar sem repetir a credencial mantém a
// que já está gravada, e exigi-la de novo a cada edição convidaria a colá-la
// em lugares onde ela não deveria aparecer.
const chatwootSchema = z.object({
  enabled: z.boolean(),
  url: z.string().url("URL inválida"),
  accountId: z.string().min(1, "Obrigatório"),
  accountToken: z.string().optional(),
  inboxId: z.string().optional(),
  inboxIdentifier: z.string().min(1, "Obrigatório"),
  markAsRead: z.boolean(),
  syncGroups: z.boolean(),
});

type ChatwootFormData = z.infer<typeof chatwootSchema>;

const campoInput =
  "w-full rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground placeholder:text-muted-foreground focus:outline-none focus:ring-2 focus:ring-ring";

export default function ChatwootSettings() {
  const { instanceId } = useParams<{ instanceId: string }>();
  const navigate = useNavigate();

  const [instance, setInstance] = useState<Instance | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [tokenGravado, setTokenGravado] = useState(false);
  const [webhookUrl, setWebhookUrl] = useState("");
  const [temConfig, setTemConfig] = useState(false);
  const hasFetchedOnce = useRef(false);

  const {
    register,
    handleSubmit,
    reset,
    formState: { errors },
  } = useForm<ChatwootFormData>({
    resolver: zodResolver(chatwootSchema),
    defaultValues: {
      enabled: true,
      url: "",
      accountId: "",
      accountToken: "",
      inboxId: "",
      inboxIdentifier: "",
      markAsRead: false,
      syncGroups: false,
    },
  });

  useEffect(() => {
    const carregar = async () => {
      if (!instanceId || hasFetchedOnce.current) return;
      hasFetchedOnce.current = true;

      try {
        const [instancia, config] = await Promise.all([
          instancesApi.fetchInstance(instanceId),
          chatwootApi.fetchChatwootConfig(instanceId),
        ]);
        setInstance(instancia);

        if (config) {
          setTemConfig(true);
          setTokenGravado(config.accountTokenSet);
          setWebhookUrl(config.webhookUrl);
          reset({
            enabled: config.enabled,
            url: config.url,
            accountId: config.accountId,
            accountToken: "",
            inboxId: config.inboxId,
            inboxIdentifier: config.inboxIdentifier,
            markAsRead: config.markAsRead,
            syncGroups: config.syncGroups,
          });
        }
      } catch (error) {
        console.error("Erro ao carregar config do Chatwoot:", error);
        toast.error(
          error instanceof Error ? error.message : "Erro ao carregar configuração"
        );
      } finally {
        setIsLoading(false);
      }
    };

    carregar();
  }, [instanceId, reset]);

  const onSubmit = async (data: ChatwootFormData) => {
    if (!instanceId) return;

    try {
      setIsSaving(true);
      const resposta = await chatwootApi.saveChatwootConfig(instanceId, {
        enabled: data.enabled,
        url: data.url.trim(),
        accountId: data.accountId.trim(),
        accountToken: data.accountToken || undefined,
        inboxId: (data.inboxId || "").trim(),
        inboxIdentifier: data.inboxIdentifier.trim(),
        markAsRead: data.markAsRead,
        syncGroups: data.syncGroups,
      });

      setTemConfig(true);
      setWebhookUrl(resposta.webhookUrl);
      if (data.accountToken) setTokenGravado(true);
      reset({ ...data, accountToken: "" });
      toast.success("Configuração do Chatwoot salva!");
    } catch (error) {
      console.error("Erro ao salvar config do Chatwoot:", error);
      toast.error(
        error instanceof Error ? error.message : "Erro ao salvar configuração"
      );
    } finally {
      setIsSaving(false);
    }
  };

  const handleDelete = async () => {
    if (!instanceId) return;
    if (!window.confirm("Apagar a configuração do Chatwoot desta instância?")) return;

    try {
      await chatwootApi.deleteChatwootConfig(instanceId);
      setTemConfig(false);
      setTokenGravado(false);
      setWebhookUrl("");
      reset({
        enabled: true,
        url: "",
        accountId: "",
        accountToken: "",
        inboxId: "",
        inboxIdentifier: "",
        markAsRead: false,
        syncGroups: false,
      });
      toast.success("Configuração apagada.");
    } catch (error) {
      console.error("Erro ao apagar config do Chatwoot:", error);
      toast.error(error instanceof Error ? error.message : "Erro ao apagar");
    }
  };

  const webhookCompleta = webhookUrl ? `${window.location.origin}${webhookUrl}` : "";

  const copiarWebhook = async () => {
    try {
      await navigator.clipboard.writeText(webhookCompleta);
      toast.success("Webhook copiado!");
    } catch {
      toast.error("Não foi possível copiar");
    }
  };

  if (isLoading) {
    return (
      <div className="flex h-full items-center justify-center">
        <div className="text-center">
          <p className="text-muted-foreground">Carregando...</p>
        </div>
      </div>
    );
  }

  return (
    <div className="h-full flex flex-col">
      {/* Header */}
      <div className="border-b border-sidebar-border bg-sidebar p-6">
        <div className="flex items-center gap-4">
          <Button
            variant="ghost"
            size="icon"
            onClick={() => navigate(`/manager/instances/${instanceId}/settings`)}
            className="text-sidebar-foreground hover:bg-sidebar-accent"
          >
            <ArrowLeft className="h-5 w-5" />
          </Button>
          <div>
            <h1 className="text-2xl font-bold text-foreground">Chatwoot</h1>
            <p className="text-sm text-muted-foreground">
              {instance?.instanceName ?? instanceId}
            </p>
          </div>
        </div>
      </div>

      {/* Conteúdo */}
      <div className="flex-1 overflow-y-auto p-6">
        <div className="max-w-4xl mx-auto space-y-6">
          <form onSubmit={handleSubmit(onSubmit)}>
            <div className="rounded-lg border border-sidebar-border bg-card p-6">
              <h2 className="text-lg font-semibold text-foreground mb-4">
                Conexão com o Chatwoot
              </h2>

              <div className="space-y-4">
                <div>
                  <label className="text-sm font-medium text-foreground">
                    URL do Chatwoot
                  </label>
                  <input
                    {...register("url")}
                    placeholder="http://chatwoot:3000"
                    className={`mt-1 ${campoInput}`}
                  />
                  {errors.url && (
                    <p className="mt-1 text-sm text-destructive">{errors.url.message}</p>
                  )}
                </div>

                <div className="grid grid-cols-2 gap-4">
                  <div>
                    <label className="text-sm font-medium text-foreground">
                      Account ID
                    </label>
                    <input
                      {...register("accountId")}
                      placeholder="1"
                      className={`mt-1 ${campoInput}`}
                    />
                    {errors.accountId && (
                      <p className="mt-1 text-sm text-destructive">
                        {errors.accountId.message}
                      </p>
                    )}
                  </div>
                  <div>
                    <label className="text-sm font-medium text-foreground">
                      Inbox ID
                    </label>
                    <input
                      {...register("inboxId")}
                      placeholder="1"
                      className={`mt-1 ${campoInput}`}
                    />
                  </div>
                </div>

                <div>
                  <label className="text-sm font-medium text-foreground">
                    Token da conta
                  </label>
                  <input
                    {...register("accountToken")}
                    type="password"
                    autoComplete="off"
                    placeholder={
                      tokenGravado ? "deixe vazio para manter o atual" : "obrigatório"
                    }
                    className={`mt-1 ${campoInput}`}
                  />
                  <p className="mt-1 text-xs text-muted-foreground">
                    O token gravado nunca é devolvido pela API; salvar sem preencher
                    mantém o atual.
                  </p>
                </div>

                <div>
                  <label className="text-sm font-medium text-foreground">
                    Inbox identifier (Channel::Api)
                  </label>
                  <input
                    {...register("inboxIdentifier")}
                    className={`mt-1 ${campoInput}`}
                  />
                  {errors.inboxIdentifier && (
                    <p className="mt-1 text-sm text-destructive">
                      {errors.inboxIdentifier.message}
                    </p>
                  )}
                </div>

                <div className="space-y-2 pt-2">
                  <label className="flex items-center gap-2 text-sm text-foreground">
                    <input type="checkbox" {...register("enabled")} />
                    Conector ligado
                  </label>
                  <label className="flex items-center gap-2 text-sm text-foreground">
                    <input type="checkbox" {...register("syncGroups")} />
                    Sincronizar grupos
                  </label>
                  <label className="flex items-center gap-2 text-sm text-foreground">
                    <input type="checkbox" {...register("markAsRead")} />
                    Marcar mensagem como lida no WhatsApp
                  </label>
                </div>
              </div>

              <div className="mt-6 flex items-center gap-2">
                <Button type="submit" disabled={isSaving}>
                  <Save className="h-4 w-4 mr-2" />
                  {isSaving ? "Salvando..." : "Salvar"}
                </Button>
                {temConfig && (
                  <Button type="button" variant="ghost" onClick={handleDelete}>
                    <Trash2 className="h-4 w-4 mr-2" />
                    Apagar
                  </Button>
                )}
              </div>
            </div>
          </form>

          {/* O webhook só existe depois de salvar: é o servidor que gera o token. */}
          {webhookCompleta && (
            <div className="rounded-lg border border-sidebar-border bg-card p-6">
              <h2 className="text-lg font-semibold text-foreground mb-2">
                Webhook da inbox
              </h2>
              <p className="text-sm text-muted-foreground mb-3">
                Configure esta URL na inbox do Chatwoot para que as respostas dos
                agentes cheguem ao WhatsApp.
              </p>
              <div className="flex items-center gap-2">
                <p className="flex-1 text-sm text-muted-foreground font-mono break-all">
                  {webhookCompleta}
                </p>
                <Button type="button" variant="ghost" size="icon" onClick={copiarWebhook}>
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
            </div>
          )}
        </div>
      </div>
    </div>
  );
}
