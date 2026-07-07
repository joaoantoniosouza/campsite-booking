# PRD --- Sistema de Gerenciamento de Agendamentos de Acampamentos para Parques Nacionais

> **Versão:** 1.1
> **Status:** Em elaboração (MVP definido)

## 1. Visão Geral

O sistema tem como objetivo gerenciar reservas de acampamentos em
parques nacionais, oferecendo uma plataforma para visitantes, empresas e
administradores realizarem e administrarem reservas de forma segura,
rápida e escalável.

O sistema foi concebido para suportar um ou mais acampamentos, controle
de capacidade por diária, overbooking configurável, check-in presencial
e gestão administrativa completa.

O sistema automatiza integralmente o processo de reserva: não há etapa
de aprovação manual pelo parque — uma reserva criada e confirmada pelo
sistema já é considerada válida, respeitando a disponibilidade de vagas.

------------------------------------------------------------------------

## 2. Objetivos

### Objetivo principal

Permitir o gerenciamento completo do processo de reserva de vagas em
acampamentos.

### Objetivos secundários

-   Evitar overbooking indevido.
-   Permitir consulta simples das reservas.
-   Facilitar cancelamentos.
-   Disponibilizar painel administrativo.
-   Garantir rastreabilidade das operações.

------------------------------------------------------------------------

# 3. Escopo do MVP

## Incluído

-   Cadastro de usuários PF e PJ
-   Login por e-mail e senha
-   Cadastro de acampamentos
-   Calendário de disponibilidade
-   Reserva por período
-   Reserva por CPF ou CNPJ
-   Código único Base62
-   Consulta por código
-   Consulta por CPF
-   Consulta do histórico do usuário autenticado
-   Alteração limitada de participantes
-   Transferência de responsabilidade da reserva
-   Cancelamento (pelo responsável, remotamente)
-   Cancelamento presencial pelo Porteiro
-   Check-in por participante
-   Reserva walk-in criada pelo Porteiro
-   Painel administrativo
-   Painel do porteiro
-   Configurações do sistema

## Fora do MVP

-   Pagamentos
-   Lista de espera
-   Notificações
-   Auditoria detalhada
-   Integrações externas

------------------------------------------------------------------------

# 4. Personas

## Visitante Pessoa Física

Pode reservar com ou sem cadastro.

## Empresa

Realiza reservas utilizando CNPJ.

## Porteiro

Realiza check-in presencial, cancelamento presencial e reservas
walk-in. Não possui permissões administrativas.

## Administrador

Gerencia todo o sistema.

------------------------------------------------------------------------

# 5. Glossário

-   **Acampamento:** local (área) onde as reservas são realizadas. Um
    parque pode ter múltiplos acampamentos, cada um com capacidade e
    configurações próprias.
-   **Diária:** cada noite ocupada por uma reserva.
-   **Período:** intervalo entre entrada e saída.
-   **Responsável:** participante responsável pela reserva.
-   **Participante:** pessoa integrante da reserva.
-   **Overbooking:** percentual adicional permitido sobre a capacidade.
-   **Reserva Temporária:** reserva bloqueando vagas até sua expiração.
-   **Reserva Walk-in:** reserva criada presencialmente pelo Porteiro,
    para visitantes sem reserva prévia, já com check-in realizado.
-   **No-show:** reserva cujo grupo não compareceu.

------------------------------------------------------------------------

# 6. Regras de Negócio

## Acampamentos

Cada acampamento possui:

-   Nome
-   Localização
-   Descrição
-   Capacidade máxima
-   Percentual de overbooking
-   Status (Ativo/Inativo)
-   Observações

A capacidade efetiva é:

    capacidade + percentual de overbooking

O percentual de overbooking é configurável individualmente por
acampamento.

Um parque pode ter múltiplos acampamentos (áreas). A capacidade total
do parque é sempre um valor agregado, calculado como a soma das
capacidades efetivas de cada acampamento individual — não é uma
configuração própria. A ocupação também é controlada de forma
independente por acampamento e por diária; o painel administrativo
deve exibir tanto a ocupação por acampamento quanto o total agregado
do parque.

------------------------------------------------------------------------

## Janela de reservas

O sistema permite reservas até dois meses à frente do mês atual.

Ao iniciar um novo mês, o mês seguinte é automaticamente liberado.

------------------------------------------------------------------------

## Reserva

Cada reserva possui:

-   Código Base62 único
-   Responsável
-   Participantes
-   Data de entrada
-   Data de saída
-   Telefones de emergência
-   Status

O código é composto por caracteres:

-   A-Z
-   a-z
-   0-9

------------------------------------------------------------------------

## Reserva temporária

Ao iniciar uma reserva:

-   vagas são bloqueadas
-   status = Pendente
-   expiração padrão = 10 minutos
-   tempo configurável

Caso expire:

-   status = Expirada
-   vagas são liberadas imediatamente

Esta regra se aplica ao fluxo padrão de reserva feito pelo visitante.
Reservas walk-in (ver seção específica) não passam por este estado.

------------------------------------------------------------------------

## Participantes

Todos os participantes devem possuir CPF.

O responsável faz parte da lista de participantes.

É obrigatório informar:

-   Nome
-   CPF

O responsável informa ainda:

-   telefone
-   e-mail (caso possua cadastro)

------------------------------------------------------------------------

## Limite de participantes

Inicialmente:

5 pessoas por reserva (PF) / 15 pessoas por reserva (PJ).

O limite é configurável.

------------------------------------------------------------------------

## Sobreposição

É proibido:

-   um responsável possuir duas reservas com períodos sobrepostos;
-   um participante integrar duas reservas com períodos sobrepostos.

A validação considera todas as diárias do período e abrange todos os
acampamentos do parque — ou seja, um mesmo CPF não pode estar em duas
reservas sobrepostas mesmo que em acampamentos diferentes.

------------------------------------------------------------------------

## Ocupação

A ocupação é calculada por pessoa, por diária, e por acampamento.

Cada diária, em cada acampamento, possui seu próprio controle de
capacidade.

A diária é contabilizada das 09:00 (entrada) às 09:00 do dia seguinte;
o dia de saída (checkout) não conta como diária ocupada.

------------------------------------------------------------------------

## Alteração de participantes

Permitida somente até o prazo configurado (24h por padrão).

Limites:

-   1--5 participantes: 1 troca
-   6--10 participantes: 2 trocas
-   11--15 participantes: 3 trocas

Troca = remover um participante e adicionar outro.

Remover participantes é permitido.

Adicionar novos participantes somente respeitando o limite de trocas.

No check-in, caso o número de participantes divergentes em relação à
lista original da reserva exceda o limite de trocas permitido, o
acesso não deve ser autorizado pelo Porteiro.

------------------------------------------------------------------------

## Transferência de responsabilidade

O responsável por uma reserva pode transferir a responsabilidade para
outro participante já integrante da mesma reserva — por exemplo, caso
não possa comparecer, mas o restante do grupo ainda deseje utilizar a
reserva.

Regras:

-   A transferência só pode ser feita para alguém que já conste como
    participante da reserva (não é permitido transferir para uma
    pessoa nova, isso seria uma alteração de participantes).
-   O novo responsável assume as obrigações do responsável original
    (contato informado, dados de titularidade da reserva).
-   A reserva mantém o mesmo código e histórico; apenas o campo de
    responsável é atualizado.
-   O antigo responsável deixa de ter permissão de gestão sobre a
    reserva (cancelamento, consulta autenticada) após a transferência,
    salvo se permanecer como participante.

------------------------------------------------------------------------

## Cancelamento

### Cancelamento remoto (pelo responsável)

Prazo padrão:

24 horas antes da entrada.

Configurável.

Ao cancelar:

-   status = Cancelada
-   vagas são liberadas imediatamente.

### Cancelamento presencial (pelo Porteiro)

O Porteiro pode cancelar uma reserva presencialmente, na presença do
responsável (por exemplo, quando o grupo desiste no momento da
chegada).

Regras:

-   Antes de efetuar o cancelamento, o Porteiro deve confirmar o CPF
    do responsável **ou** o código da reserva, para garantir que a
    pessoa presente é de fato o responsável pela reserva.
-   Sem essa confirmação, o cancelamento presencial não pode ser
    concluído.
-   O prazo de 24 horas de antecedência não se aplica ao cancelamento
    presencial, já que ocorre no momento da chegada.
-   Ao cancelar, vagas são liberadas imediatamente, da mesma forma que
    no cancelamento remoto.

------------------------------------------------------------------------

## Contato de emergência

Obrigatório informar pelo menos um contato contendo:

-   Nome
-   Telefone
-   Grau de parentesco

O contato deve ser externo ao parque que possa atender 24h.

------------------------------------------------------------------------

## Consulta

Uma reserva poderá ser localizada por:

-   código Base62
-   CPF do responsável
-   CPF de qualquer participante
-   histórico do usuário autenticado

------------------------------------------------------------------------

## Reserva Walk-in

O Porteiro pode criar uma reserva diretamente no painel do porteiro
para visitantes que chegam sem reserva prévia.

Regras:

-   A criação só é permitida se houver vagas disponíveis no
    acampamento e na(s) diária(s) desejada(s), considerando a
    capacidade efetiva (incluindo overbooking).
-   A reserva walk-in segue as mesmas regras de cadastro de
    participantes (CPF obrigatório, contato de emergência, limite de
    participantes).
-   Diferente do fluxo comum, a reserva walk-in não passa pelo estado
    "Pendente" com expiração de 10 minutos.
-   A reserva walk-in nasce diretamente com status = Check-in
    Realizado, já que é criada e confirmada pelo próprio Porteiro no
    momento da entrada.
-   Aplica-se a mesma validação de sobreposição (CPF do responsável ou
    de qualquer participante não pode ter reserva em período
    sobreposto em qualquer acampamento do parque).

------------------------------------------------------------------------

# 7. Cadastros

## Pessoa Física (opcional)

Campos:

-   Nome
-   CPF
-   Data de nascimento
-   E-mail
-   Telefone
-   Senha

Benefícios:

-   login
-   histórico
-   cancelamento facilitado
-   dados pré-preenchidos

## Pessoa Jurídica (obrigatório)

Campos:

Empresa

-   Razão social
-   CNPJ
-   E-mail
-   Senha

Responsável legal

-   Nome
-   CPF
-   E-mail
-   Telefone

------------------------------------------------------------------------

# 8. Check-in

Realizado pelo Porteiro.

Permissões:

-   localizar reserva
-   registrar presença
-   criar reserva walk-in
-   cancelar reserva presencialmente

Regras:

-   responsável deve obrigatoriamente estar presente;
-   presença registrada individualmente;
-   participantes ausentes permanecem registrados;
-   caso a divergência de participantes exceda o limite de trocas
    permitido, o acesso não é autorizado.

Fluxos:

Pendente → Check-in realizado → Finalizada

Fluxos alternativos:

Pendente → Cancelada

Pendente → No-show

Pendente → Expirada

Fluxo específico da reserva walk-in:

(Criada pelo Porteiro) → Check-in realizado → Finalizada

------------------------------------------------------------------------

# 9. Painel Administrativo

## Dashboard

-   ocupação (por acampamento e agregada por parque)
-   reservas
-   cancelamentos
-   no-shows
-   acampamentos

## Gestão

-   Acampamentos
-   Reservas
-   Usuários
-   Empresas
-   Porteiros
-   Configurações

------------------------------------------------------------------------

# 10. Configurações

-   limite por reserva
-   janela de reservas
-   prazo de cancelamento
-   prazo de alteração
-   tempo da reserva temporária
-   regras de trocas
-   overbooking

------------------------------------------------------------------------

# 11. Requisitos Funcionais

-   RF01 Cadastrar acampamentos
-   RF02 Gerenciar usuários
-   RF03 Gerenciar empresas
-   RF04 Criar reservas
-   RF05 Cancelar reservas (remoto e presencial)
-   RF06 Alterar participantes
-   RF07 Consultar reservas
-   RF08 Calcular disponibilidade por diária e por acampamento
-   RF09 Controlar overbooking
-   RF10 Registrar check-in
-   RF11 Gerenciar configurações
-   RF12 Criar reserva walk-in (Porteiro)
-   RF13 Transferir responsabilidade da reserva

------------------------------------------------------------------------

# 12. Requisitos Não Funcionais

-   Consistência transacional
-   Controle de concorrência
-   Escalabilidade
-   Disponibilidade
-   Segurança
-   LGPD
-   Tempo de resposta inferior a 200 ms para consulta de disponibilidade

------------------------------------------------------------------------

# 13. Casos Extremos

-   Duas reservas simultâneas para a última vaga.
-   Expiração de reserva temporária.
-   Cancelamento liberando vagas.
-   Cancelamento presencial sem confirmação válida de CPF/código.
-   Alteração simultânea de participantes.
-   Transferência de responsabilidade para participante que também
    deseja cancelar em seguida.
-   Reserva walk-in feita quando a vaga se libera por outro
    cancelamento simultâneo.
-   Mudança de capacidade do acampamento.
-   Mudança do percentual de overbooking.

------------------------------------------------------------------------

# 14. Roadmap

## MVP

Todo o escopo descrito neste documento.

## Pós-MVP

-   Pagamentos
-   Lista de espera
-   Auditoria detalhada
-   Notificações
-   Relatórios avançados
-   Integrações externas
