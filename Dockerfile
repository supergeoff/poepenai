# Étape 1: Construction de l'application avec Mage
FROM golang:latest AS builder

# Installer les dépendances système et Mage
RUN go install github.com/magefile/mage@latest

# Copier le code source
WORKDIR /app
COPY . .

# Construire le binaire avec Mage
RUN mage build

# Étape 2: Création de l'image d'exécution finale
FROM alpine:latest

# Copier le binaire depuis le builder
COPY --from=builder /app/dist/poepenai /usr/local/bin/poepenai

# Définir les paramètres d'exécution
EXPOSE 8080
CMD ["poepenai", "start"]