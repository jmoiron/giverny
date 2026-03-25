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
    var pendingForm = null;

    $(document).on('click', '[data-submit]', function() {
        var form = document.getElementById($(this).data('submit'));
        if (!form) return;
        var msg = $(this).data('confirm');
        if (msg) {
            pendingForm = form;
            $('#confirm-message').text(msg);
            $modal.addClass('active');
        } else {
            form.submit();
        }
    });

    $('#confirm-cancel').on('click', function() {
        $modal.removeClass('active');
        pendingForm = null;
    });

    $('#confirm-ok').on('click', function() {
        $modal.removeClass('active');
        if (pendingForm) pendingForm.submit();
        pendingForm = null;
    });

    $modal.on('click', function(e) {
        if (e.target === this) {
            $modal.removeClass('active');
            pendingForm = null;
        }
    });

    $(document).on('keydown', function(e) {
        if (e.key !== 'Escape') return;
        if ($modal.hasClass('active')) {
            $modal.removeClass('active');
            pendingForm = null;
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

});
