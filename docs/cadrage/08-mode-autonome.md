# 12. Mode autonome (post-MVP)

Le MVP est interactif et supervisé : la session complète vit dans le REPL de
Claude Code, l'humain répond au terminal, rien à concevoir. Le terminal
rattaché par `container exec -it` est un PTY dont les octets traversent bruts,
séquences d'échappement comprises (D10) : c'est une exception à R6 (S17),
acceptée parce qu'un humain regarde ce terminal et que le wrapper n'y interprète
rien lui-même, et permanente puisque le régime attaché de `send` reste (§11) ;
le mode autonome, sans terminal, n'en hérite pas. Ce paragraphe conçoit
le régime **autonome** qui vient après — quand aucun humain n'est dans la boucle.
C'est de la conception, cohérente avec P0 et P6 ; elle n'est pas requise pour le
MVP.

**Deux natures d'interruption, une seule légitime.** Une demande de *permission
d'agir* (« puis-je parser ce site, lancer cette commande ? ») est
**structurellement vide** ici : la boîte est jetable, l'egress est ouvert (P1) et
la sortie est filtrée à la récupération (R7′/R8). Dedans, tout est permis parce
que sans conséquence ; dehors, rien n'est demandé parce que tout est verrouillé à
la sortie. Il n'y a donc pas de canal de permission — l'architecture a répondu à
toutes les permissions d'avance. Une demande de *clarification du sens* (« que
faut-il faire ici ? ») est d'une autre nature : c'est de l'information que seul
l'humain détient, qu'aucune architecture ne pré-décide. Elle est légitime, mais
**asynchrone** — l'agent ne bloque pas, il l'émet et le run se termine.

**Trois états terminaux, imposés par le harnais (P0).** Un run autonome se
termine dans exactement un de :

- **`push`** — le travail est fait ; le diff revient (R8) et alimente la branche.
- **`question`** — une ambiguïté de *sens* rend le travail indécidable ; la
  question devient un commentaire de MR.
- **`blocked`** — une borne atteinte (tours, budget, durée) ou un échec ; devient
  un commentaire « je n'ai pas pu, voici pourquoi », jamais un silence.

L'existence de ces états et le schéma de sortie sont **vérifiables**, donc du
harnais : le wrapper les impose via `--output-format json` et un contrat dans le
prompt système, puis route la sortie (pousser, commenter, signaler). La
*propension* de l'agent à emprunter `question` plutôt que `push` est du **ton**,
donc du projet (P0) : « ne demande que si tu es réellement bloqué sur le sens ;
sinon fais ta meilleure tentative et documente tes hypothèses » vit dans le
`CLAUDE.md`, pas dans le harnais.

**Le fil de commentaires de la MR *est* la session, rendue asynchrone.** Question
de l'agent → commentaire ; réponse humaine → commentaire, qui **déclenche un tour
de plus** de l'agent. Trois verbes aux rôles disjoints. `run` crée la sandbox, la
VM et l'agent dedans, et rien d'autre. Le **verbe d'interaction**, `send`, parle
à l'agent de cette sandbox, autant de fois qu'on veut : avec un prompt il envoie
un tour, piloté et borné (autonome), sans prompt il attache un terminal
(conversationnel) ; c'est lui qui porte la reprise d'une session (`--resume`),
jamais `run`. `stop` arrête la VM, `kill` la tue : le coupe-circuit
asymétrique pour un run emballé (R4/R5). Autour, les verbes de docker que les
développeurs connaissent, `list` (alias `ls` et `ps`), `rm`, `logs`. En local on
tape `send` ; en mode forge, l'amont de D8 traduit « réponse au commentaire » en
ce même verbe, et le wrapper reste sans surface entrante.

**Reprendre après `stop` impose de persister l'état de session hors de la
boîte.** Tant que la VM tourne, la session vit dedans et le verbe d'interaction
l'enchaîne ; une fois la VM arrêtée (R5), la transcription qu'une reprise exige
doit survivre **côté hôte**. Résidu *voulu*, pas entorse à R5. Mais c'est une
instance de **S22** (état partagé entre runs) : le magasin de sessions doit être
**cloisonné par session** — l'état d'une session ne fuit jamais dans une autre —
au même titre que le broker interdit tout état exploitable entre runs.

**Une session autonome est bornée par construction**, puisqu'aucun humain ne
l'arrêtera : `--max-turns`, budget, durée (R4). « Gérer une session entière » en
autonome, c'est « une session bornée qui va au bout seule », pas « une session
ouverte qui attend ».
