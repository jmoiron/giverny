$(function() {
    function applyTheme(theme) {
        if (theme === 'dark') document.documentElement.classList.add('dark');
        else document.documentElement.classList.remove('dark');
    }

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

    $('#theme-toggle').on('click', function() {
        var cur = localStorage.getItem('theme') || 'light';
        var next = cur === 'dark' ? 'light' : 'dark';
        localStorage.setItem('theme', next);
        applyTheme(next);
    });
});
