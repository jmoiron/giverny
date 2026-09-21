(function () {
    'use strict';

    if (!window.PublicKeyCredential || !window.isSecureContext) {
        var unsupported = document.getElementById('passkey-unsupported');
        var unsupportedButton = document.getElementById('passkey-register');
        if (unsupported) unsupported.hidden = false;
        if (unsupportedButton) unsupportedButton.disabled = true;
        return;
    }

    function bytes(value) {
        var padded = value.replace(/-/g, '+').replace(/_/g, '/');
        while (padded.length % 4) padded += '=';
        var raw = atob(padded);
        var result = new Uint8Array(raw.length);
        for (var i = 0; i < raw.length; i++) result[i] = raw.charCodeAt(i);
        return result;
    }

    function encoded(value) {
        var raw = '';
        var data = new Uint8Array(value);
        for (var i = 0; i < data.length; i++) raw += String.fromCharCode(data[i]);
        return btoa(raw).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '');
    }

    function creationOptions(options) {
        options.publicKey.challenge = bytes(options.publicKey.challenge);
        options.publicKey.user.id = bytes(options.publicKey.user.id);
        (options.publicKey.excludeCredentials || []).forEach(function (item) { item.id = bytes(item.id); });
        return options.publicKey;
    }

    function requestOptions(options) {
        options.publicKey.challenge = bytes(options.publicKey.challenge);
        (options.publicKey.allowCredentials || []).forEach(function (item) { item.id = bytes(item.id); });
        return options.publicKey;
    }

    function registerPasskey() {
        return postJSON('/auth/webauthn/register/begin').then(function (options) {
            return navigator.credentials.create({ publicKey: creationOptions(options) });
        }).then(function (credential) {
            return postJSON('/auth/webauthn/register/finish', {
                id: credential.id,
                rawId: encoded(credential.rawId),
                type: credential.type,
                response: {
                    clientDataJSON: encoded(credential.response.clientDataJSON),
                    attestationObject: encoded(credential.response.attestationObject)
                }
            });
        });
    }

    function postJSON(url, body) {
        return fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: body ? JSON.stringify(body) : undefined })
            .then(function (response) {
                if (!response.ok) return response.text().then(function (message) { throw new Error(message || 'request failed'); });
                return response.json();
            });
    }

    function showStatus(element, message, error) {
        element.textContent = message;
        element.hidden = false;
        element.classList.toggle('error', !!error);
    }

    var register = document.getElementById('passkey-register');
    var list = document.getElementById('passkey-list');
    var status = document.getElementById('passkey-status');
    if (register && list && status) {
        function loadCredentials() {
            fetch('/auth/webauthn/credentials').then(function (response) { return response.json(); }).then(function (credentials) {
                credentials = Array.isArray(credentials) ? credentials : [];
                list.innerHTML = '';
                credentials.forEach(function (credential) {
                    var item = document.createElement('li');
                    item.appendChild(document.createTextNode(credential.name || 'passkey'));
                    var remove = document.createElement('button');
                    remove.type = 'button'; remove.textContent = 'remove';
                    remove.addEventListener('click', function () {
                        postJSON('/auth/webauthn/credentials/' + credential.id + '/delete').then(loadCredentials);
                    });
                    item.appendChild(remove); list.appendChild(item);
                });
            });
        }
        loadCredentials();
        register.addEventListener('click', function () {
            register.disabled = true;
            registerPasskey().then(function () { showStatus(status, 'passkey added', false); loadCredentials(); })
                .catch(function (error) { showStatus(status, error.message, true); })
                .finally(function () { register.disabled = false; });
        });
    }

    var prompt = document.getElementById('passkey-prompt');
    if (prompt && document.body.getAttribute('data-disable-passkey-prompt') !== '1') {
        fetch('/auth/webauthn/prompt').then(function (response) {
            if (!response.ok) throw new Error('could not check passkey prompt state');
            return response.json();
        }).then(function (state) {
            if (!state.pending) return null;
            return fetch('/auth/webauthn/credentials');
        }).then(function (response) {
            if (!response) return null;
            if (!response.ok) throw new Error('could not check passkeys');
            return response.json();
        }).then(function (credentials) {
            if (!credentials) return;
            if (!Array.isArray(credentials) || !credentials.length) prompt.hidden = false;
        }).catch(function () {});
        function dismissPrompt() {
            prompt.hidden = true;
        }
        document.getElementById('passkey-prompt-dismiss').addEventListener('click', dismissPrompt);
        document.getElementById('passkey-prompt-never').addEventListener('click', function () {
            var button = document.getElementById('passkey-prompt-never');
            button.disabled = true;
            postJSON('/auth/webauthn/prompt/never').then(dismissPrompt).catch(function (error) {
                button.disabled = false;
                var promptStatus = document.getElementById('passkey-prompt-status');
                promptStatus.textContent = error.message;
                promptStatus.hidden = false;
            });
        });
        prompt.addEventListener('click', function (event) {
            if (event.target === prompt) dismissPrompt();
        });
        document.addEventListener('keydown', function (event) {
            if (event.key === 'Escape' && !prompt.hidden) dismissPrompt();
        });
        document.getElementById('passkey-prompt-create').addEventListener('click', function () {
            var button = document.getElementById('passkey-prompt-create');
            var promptStatus = document.getElementById('passkey-prompt-status');
            button.disabled = true;
            registerPasskey().then(function () {
                prompt.hidden = true;
            }).catch(function (error) {
                promptStatus.textContent = error.name === 'NotAllowedError' || error.name === 'AbortError'
                    ? 'The operation failed or was canceled.'
                    : error.message;
                promptStatus.hidden = false;
                button.disabled = false;
            });
        });
    }

    var login = document.getElementById('passkey-login');
    if (login) {
        login.hidden = false;
        var loginDivider = document.getElementById('passkey-login-divider');
        if (loginDivider) loginDivider.hidden = false;
        login.addEventListener('click', function () {
            login.disabled = true;
            postJSON('/auth/webauthn/login/begin').then(function (options) {
                return navigator.credentials.get({ publicKey: requestOptions(options) });
            }).then(function (credential) {
                return postJSON('/auth/webauthn/login/finish', {
                    id: credential.id,
                    rawId: encoded(credential.rawId),
                    type: credential.type,
                    response: {
                        clientDataJSON: encoded(credential.response.clientDataJSON),
                        authenticatorData: encoded(credential.response.authenticatorData),
                        signature: encoded(credential.response.signature),
                        userHandle: credential.response.userHandle ? encoded(credential.response.userHandle) : null
                    }
                });
            }).then(function () {
                var redirect = new URLSearchParams(window.location.search).get('redirect') || '/';
                window.location.assign(redirect);
            }).catch(function (error) { login.disabled = false; window.alert(error.message); });
        });
    }
}());
