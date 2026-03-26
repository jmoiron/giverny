$(function() {
    function applyTheme(theme) {
        if (theme === 'dark') document.documentElement.classList.add('dark');
        else document.documentElement.classList.remove('dark');
    }

    function channelLuminance(v) {
        if (v <= 0.03928) return v / 12.92;
        return Math.pow((v + 0.055) / 1.055, 2.4);
    }

    function labelTextClass(color) {
        var hex = String(color || '').trim().replace(/^#/, '');
        if (hex.length !== 6) return 'fg-dark';
        var r = parseInt(hex.slice(0, 2), 16);
        var g = parseInt(hex.slice(2, 4), 16);
        var b = parseInt(hex.slice(4, 6), 16);
        var luminance = 0.2126 * channelLuminance(r / 255) +
            0.7152 * channelLuminance(g / 255) +
            0.0722 * channelLuminance(b / 255);
        return luminance > 0.58 ? 'fg-dark' : 'fg-light';
    }

    applyTheme(localStorage.getItem('theme') || 'light');

    // User dropdown (declared early so the shared Escape handler can reference them).
    var $trigger = $('#user-menu-trigger');
    var $dropdown = $('#user-dropdown');

    // Reusable confirmation modal
    var $modal = $('#confirm-modal');
    var pendingAction = null;

    function hideConfirmModal() {
        $modal.removeClass('active');
        pendingAction = null;
    }

    window.showConfirmModal = function(msg, action) {
        pendingAction = action || null;
        $('#confirm-message').text(msg || '');
        $modal.addClass('active');
    };

    $(document).on('click', '[data-submit]', function() {
        var form = document.getElementById($(this).data('submit'));
        if (!form) return;
        var msg = $(this).data('confirm');
        if (msg) {
            window.showConfirmModal(msg, function() { form.submit(); });
        } else {
            form.submit();
        }
    });

    $('#confirm-cancel').on('click', function() {
        hideConfirmModal();
    });

    $('#confirm-ok').on('click', function() {
        var action = pendingAction;
        hideConfirmModal();
        if (action) action();
    });

    $modal.on('click', function(e) {
        if (e.target === this) {
            hideConfirmModal();
        }
    });

    $(document).on('keydown', function(e) {
        if (e.key !== 'Escape') return;
        if ($modal.hasClass('active')) {
            hideConfirmModal();
        }
        $('.modal-overlay.active').removeClass('active');
        $trigger.removeClass('open');
        $dropdown.removeClass('open');
    });

    $(document).on('click', '.copy-btn', function() {
        var text = $(this).data('copy');
        navigator.clipboard.writeText(text).then(() => {
            var $el = $(this);
            $el.removeClass('fa-copy').addClass('fa-check');
            setTimeout(() => $el.removeClass('fa-check').addClass('fa-copy'), 1500);
        });
    });

    $(document).on('click', '.nav-icon-link[href="#"]', function(e) {
        e.preventDefault();
    });

    $(document).on('input', 'input.error', function() {
        $(this).removeClass('error');
    });

    $(document).on('input', '.admin-labels input[name=color], .admin-labels input[name=title], .admin-labels input[name=description]', function() {
        var $row = $(this).closest('tr');
        var $chip = $row.find('.label-pill').first();
        if (!$chip.length) return;

        var color = $row.find('input[name=color]').val() || '#888888';
        var title = $row.find('input[name=title]').val() || '';
        var description = $row.find('input[name=description]').val() || '';

        $chip.attr('style', '--label-color: ' + color);
        $chip.removeClass('fg-light fg-dark').addClass(labelTextClass(color));
        $chip.attr('title', description);
        $chip.text(title);
    });

    $('#theme-toggle').on('click', function() {
        var cur = localStorage.getItem('theme') || 'light';
        var next = cur === 'dark' ? 'light' : 'dark';
        localStorage.setItem('theme', next);
        applyTheme(next);
    });

    $trigger.on('click', function(e) {
        e.stopPropagation();
        $trigger.toggleClass('open');
        $dropdown.toggleClass('open');
    });

    $(document).on('click', function(e) {
        if (!$trigger.length) return;
        if (!$trigger[0].contains(e.target) && !$dropdown[0].contains(e.target)) {
            $trigger.removeClass('open');
            $dropdown.removeClass('open');
        }
    });

    function syncAvatarPreview(url) {
        var $preview = $('#settings-avatar-preview');
        if (!$preview.length) return;
        var normalized = String(url || '').trim();
        if ($preview.is('img')) {
            if (normalized) {
                $preview.attr('src', normalized);
                return;
            }
            var $fallback = $('<span class="settings-avatar avatar-fallback" id="settings-avatar-preview"></span>');
            $preview.replaceWith($fallback);
            return;
        }
        if (normalized) {
            var $img = $('<img class="settings-avatar" id="settings-avatar-preview" alt="">');
            $img.attr('src', normalized);
            $preview.replaceWith($img);
        }
    }

    function setSettingsStatus(kind, text) {
        var $status = $('#settings-status');
        if (!$status.length) return;
        if (!text) {
            $status.attr('hidden', 'hidden').removeClass('error').text('');
            return;
        }
        $status.removeAttr('hidden').removeClass('error').addClass(kind || '').text(text);
    }

    var settingsSaveTimers = {};
    var settingsSaveSeq = 0;
    var avatarDirty = false;

    function saveUserSetting(field) {
        var $form = $('#user-settings-form');
        if (!$form.length) return;
        var seq = ++settingsSaveSeq;
        var formData = new URLSearchParams();
        if (field === 'profile_image_uri') {
            formData.set('profile_image_uri_present', '1');
            formData.set('profile_image_uri', $('#profile-image-uri').val() || '');
        } else if (field === 'timezone') {
            formData.set('timezone_present', '1');
            formData.set('timezone', $('#timezone-select').val() || '');
        } else if (field === 'auto_assign_cards') {
            formData.set('auto_assign_cards_present', '1');
            if ($('#auto-assign-cards').is(':checked')) formData.set('auto_assign_cards', '1');
        } else {
            return;
        }
        setSettingsStatus('', '');
        fetch($form.attr('action'), {
            method: 'POST',
            body: formData.toString(),
            headers: {
                'Accept': 'application/json',
                'Content-Type': 'application/x-www-form-urlencoded; charset=UTF-8'
            }
        })
            .then(function(resp) {
                return resp.json().catch(function() {
                    return {};
                }).then(function(data) {
                    if (!resp.ok) {
                        throw new Error(data.error || 'save failed');
                    }
                    return data;
                });
            })
            .then(function(data) {
                if (seq !== settingsSaveSeq) return;
                if (field === 'profile_image_uri' && typeof data.profile_image_uri !== 'undefined') {
                    $('#profile-image-uri').val(data.profile_image_uri || '');
                    syncAvatarPreview(data.profile_image_uri || '');
                }
                if (field === 'timezone' && typeof data.timezone !== 'undefined') {
                    $('#timezone-select').val(data.timezone || 'UTC');
                }
                if (field === 'auto_assign_cards' && typeof data.auto_assign_cards !== 'undefined') {
                    $('#auto-assign-cards').prop('checked', !!data.auto_assign_cards);
                }
                if (field === 'profile_image_uri') avatarDirty = false;
            })
            .catch(function(err) {
                if (seq !== settingsSaveSeq) return;
                setSettingsStatus('error', err.message || 'save failed');
            });
    }

    function scheduleSettingsSave(delay, field) {
        window.clearTimeout(settingsSaveTimers[field]);
        settingsSaveTimers[field] = window.setTimeout(function() {
            saveUserSetting(field);
        }, delay || 0);
    }

    $('#profile-image-uri').on('input', function() {
        syncAvatarPreview($(this).val());
        avatarDirty = true;
        setSettingsStatus('', '');
    });

    $('#avatar-url-save').on('click', function() {
        avatarDirty = true;
        scheduleSettingsSave(0, 'profile_image_uri');
    });

    $('#timezone-autodetect').on('click', function() {
        var tz = '';
        try {
            tz = Intl.DateTimeFormat().resolvedOptions().timeZone || '';
        } catch (err) {
            tz = '';
        }
        if (!tz) return;
        var $select = $('#timezone-select');
        if (!$select.find('option[value="' + tz + '"]').length) {
            $select.append('<option value="' + tz + '">' + tz + '</option>');
        }
        $select.val(tz);
        scheduleSettingsSave(0, 'timezone');
    });

    $('#timezone-select').on('change', function() {
        scheduleSettingsSave(0, 'timezone');
    });

    $('#auto-assign-cards').on('change', function() {
        scheduleSettingsSave(0, 'auto_assign_cards');
    });

    var $avatarUploadModal = $('#avatar-upload-modal');
    var $avatarDropzone = $('#avatar-upload-dropzone');
    var $avatarInput = $('#avatar-upload-input');
    var $avatarStatus = $('#avatar-upload-status');

    function closeAvatarUploadModal() {
        $avatarUploadModal.removeClass('active');
        $avatarDropzone.removeClass('is-dragging');
        $avatarStatus.removeClass('is-error').text('');
        $avatarInput.val('');
    }

    function uploadAvatarFile(file) {
        if (!file) return;
        var formData = new FormData();
        formData.append('file', file);
        $avatarStatus.removeClass('is-error').text('uploading...');
        fetch('/user/settings/avatar-upload', {
            method: 'POST',
            body: formData
        })
            .then(function(resp) {
                if (!resp.ok) {
                    return resp.text().then(function(text) {
                        throw new Error(text || 'upload failed');
                    });
                }
                return resp.json();
            })
            .then(function(data) {
                $('#profile-image-uri').val(data.url || '');
                syncAvatarPreview(data.url || '');
                avatarDirty = true;
                scheduleSettingsSave(0, 'profile_image_uri');
                closeAvatarUploadModal();
            })
            .catch(function(err) {
                $avatarStatus.addClass('is-error').text(err.message || 'upload failed');
            });
    }

    $('#avatar-upload-open').on('click', function() {
        $avatarUploadModal.addClass('active');
    });

    $('#avatar-upload-close').on('click', function(e) {
        e.preventDefault();
        closeAvatarUploadModal();
    });

    $avatarUploadModal.on('click', function(e) {
        if (e.target === this) closeAvatarUploadModal();
    });

    $avatarDropzone.on('click', function() {
        $avatarInput.trigger('click');
    });

    $avatarDropzone.on('dragenter dragover', function(e) {
        e.preventDefault();
        $avatarDropzone.addClass('is-dragging');
    });

    $avatarDropzone.on('dragleave', function(e) {
        e.preventDefault();
        if (e.target === this) {
            $avatarDropzone.removeClass('is-dragging');
        }
    });

    $avatarDropzone.on('drop', function(e) {
        e.preventDefault();
        $avatarDropzone.removeClass('is-dragging');
        var files = null;
        if (e.originalEvent && e.originalEvent.dataTransfer) files = e.originalEvent.dataTransfer.files;
        else if (e.dataTransfer) files = e.dataTransfer.files;
        if (files && files.length) uploadAvatarFile(files[0]);
    });

    $avatarInput.on('change', function() {
        if (this.files && this.files.length) uploadAvatarFile(this.files[0]);
    });

});
