# Вендорные пакеты

`@villetacore/velessoft-ui` и `@villetacore/velessoft-tokens` 0.2.0 ещё не
опубликованы в npm, поэтому web/package.json ставит их из архивов здесь.
Так `npm ci`, CI и Docker-сборка работают без доступа к соседнему репозиторию.

Источник — https://github.com/villetacore/velessoft-ui, коммит 46a7039.
Архивы собраны командой `npm pack -w @villetacore/velessoft-ui -w @villetacore/velessoft-tokens`.

Обновление: собрать новые архивы в velessoft-ui, положить сюда, удалить старые и
выполнить в web/ `npm install ./vendor/<ui>.tgz ./vendor/<tokens>.tgz`.
Когда пакеты появятся в npm, заменить `file:vendor/…` на версию из реестра
и удалить этот каталог.
