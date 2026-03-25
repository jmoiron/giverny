$(function() {
    var board = $('#board-outer').data('slug');
    if (!board) return;

    function post(url, data) {
        var body = data instanceof FormData ? data : new URLSearchParams(data);
        return fetch(url, { method: 'POST', body: body });
    }

    // --- Add column toggle ---
    $('#add-column-btn').on('click', function() {
        $(this).hide();
        $('.add-column-form').show().find('input[name=name]')[0].focus();
    });

    $(document).on('click', '.add-column-form .btn-cancel', function() {
        $('.add-column-form').hide()[0].reset();
        $('#add-column-btn').show();
    });

    // --- Add card inline ---
    $(document).on('click', '.add-card-btn', function() {
        var $col = $(this).closest('.board-column');
        $(this).hide();
        $col.find('.add-card-form').show().find('input[name=title]')[0].focus();
    });

    $(document).on('keydown', '.add-card-form input, .add-column-form input', function(e) {
        if (e.key === 'Escape') $(this).closest('form').find('.btn-cancel')[0].click();
    });

    $(document).on('click', '.add-card-form .btn-cancel', function() {
        var $form = $(this).closest('.add-card-form');
        $form.hide()[0].reset();
        $form.closest('.board-column').find('.add-card-btn').show();
    });

    $(document).on('submit', '.add-card-form', function(e) {
        e.preventDefault();
        var $form = $(this);
        var colId = $form.data('col');
        post('/boards/' + board + '/columns/' + colId + '/cards', new URLSearchParams(new FormData($form[0])))
            .then(function(r) { return r.text(); })
            .then(function(html) {
                $form.closest('.board-column').find('.col-cards').append(html);
                $form.hide()[0].reset();
                $form.closest('.board-column').find('.add-card-btn').show();
            });
    });

    // --- Card detail modal ---
    var $cardModal = $('#card-modal');

    $(document).on('click', '.kanban-card', function() {
        var cardId = $(this).data('id');
        fetch('/boards/' + board + '/cards/' + cardId)
            .then(function(r) { return r.text(); })
            .then(function(html) {
                $cardModal.find('.card-modal-inner').html(html);
                $cardModal.addClass('active');
            });
    });

    // Close card modal
    $(document).on('click', '#card-modal-close', function() {
        $cardModal.removeClass('active');
    });

    $cardModal.on('click', function(e) {
        if (e.target === this) $cardModal.removeClass('active');
    });

    // Save card
    $(document).on('submit', '#card-detail-form', function(e) {
        e.preventDefault();
        var cardId = $(this).data('id');
        post('/boards/' + board + '/cards/' + cardId, new URLSearchParams(new FormData(this)))
            .then(function(r) { return r.json(); })
            .then(function(data) {
                $('.kanban-card[data-id=' + cardId + '] .card-title').text(data.title);
                $cardModal.removeClass('active');
            });
    });

    // Move card (column select change)
    $(document).on('change', '#card-move-select', function() {
        var $form = $(this).closest('#card-detail-form');
        var cardId = $form.data('id');
        var colId = $(this).val();
        post('/boards/' + board + '/cards/' + cardId + '/move', { column_id: colId, position: 0 })
            .then(function() { location.reload(); });
    });

    // Archive card
    $(document).on('click', '#card-archive-btn', function() {
        var cardId = $(this).data('card-id');
        post('/boards/' + board + '/cards/' + cardId + '/archive', {})
            .then(function() {
                $('.kanban-card[data-id=' + cardId + ']').remove();
                $cardModal.removeClass('active');
            });
    });
});
